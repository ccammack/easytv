package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/httpapi"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"log"
	"math"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Owner serializes application commands, preview commits and player events.
// After Run begins, its controller and player belong only to it. Channel
// feedback is independently serialized by the coordinator's preview lock.
// Image timers and preparation results are handled in the same owner loop.
type Owner struct {
	controller   *playback.Controller
	app          *Coordinator
	player       *mpvplayer.Player
	report       func(error)
	requests     chan ownerRequest
	done         chan struct{}
	started      atomic.Bool
	scan         func(context.Context) ([]catalog.Channel, error)
	retryDelay   time.Duration
	claims       chan channelClaim
	claimResults chan channelClaim
	claimsDone   chan struct{}
	inputReady   chan struct{}
	accepting    bool // protected by app.mu
	inputWake    chan struct{}
}

type OwnerCommand struct {
	Volume    float64
	Muted     bool
	Kind      string // status, start, stop, channel, volume, mute, cec, or rescan
	Direction int
	Message   cec.ReceivedMessage
	Adapter   cec.AdapterInfo
}

type OwnerResult struct {
	Status     httpapi.PlaybackStatus
	State      state.State
	Running    bool
	Generation uint64
	Reason     string
	Err        error
}

type ownerRequest struct {
	command OwnerCommand
	reply   chan OwnerResult
}

type scanResult struct {
	channels []catalog.Channel
	err      error
}

func NewOwner(c *playback.Controller, a *Coordinator, p *mpvplayer.Player, report func(error)) (*Owner, error) {
	if c == nil || a == nil || p == nil || report == nil {
		return nil, errors.New("controller, coordinator, player and error reporter required")
	}
	// The coordinator must operate on this exact controller.
	if a.controller != c {
		return nil, errors.New("coordinator/controller mismatch")
	}
	return &Owner{controller: c, app: a, player: p, report: report, requests: make(chan ownerRequest), done: make(chan struct{}),
		claims: make(chan channelClaim, 64), claimResults: make(chan channelClaim, 64), claimsDone: make(chan struct{}), inputReady: make(chan struct{}), inputWake: make(chan struct{}, 1)}, nil
}

// Submit waits for an owner response except for channel feedback, which is
// queued independently. Cancellation after acceptance does not roll back work.
func (o *Owner) Submit(ctx context.Context, command OwnerCommand) (OwnerResult, error) {
	if err := ctx.Err(); err != nil {
		return OwnerResult{}, err
	}
	select {
	case <-o.done:
		return OwnerResult{}, errors.New("owner stopped")
	default:
	}
	if command.Kind == "channel" {
		select {
		case <-ctx.Done():
			return OwnerResult{}, ctx.Err()
		case <-o.done:
			return OwnerResult{}, errors.New("owner stopped")
		case <-o.inputReady:
		}
		if err := ctx.Err(); err != nil {
			return OwnerResult{}, err
		}
		select {
		case <-o.done:
			return OwnerResult{}, errors.New("owner stopped")
		default:
		}
		return OwnerResult{Err: o.queueChannel(command.Direction, time.Now())}, nil
	}
	request := ownerRequest{command: command, reply: make(chan OwnerResult, 1)}
	select {
	case <-ctx.Done():
		return OwnerResult{}, ctx.Err()
	case <-o.done:
		return OwnerResult{}, errors.New("owner stopped")
	case o.requests <- request:
	}
	select {
	case result := <-request.reply:
		return result, nil
	case <-ctx.Done():
		return OwnerResult{}, ctx.Err()
	case <-o.done:
		select {
		case result := <-request.reply:
			return result, nil
		default:
			return OwnerResult{}, errors.New("owner stopped")
		}
	}
}

func (o *Owner) Run(ctx context.Context) (result error) {
	if !o.started.CompareAndSwap(false, true) {
		return errors.New("owner can run only once")
	}
	defer close(o.done)
	claimCtx, cancelClaims := context.WithCancel(ctx)
	go o.runClaims(claimCtx)
	o.app.mu.Lock()
	o.accepting = true
	o.app.mu.Unlock()
	close(o.inputReady)
	defer func() {
		o.app.mu.Lock()
		o.accepting = false
		o.app.mu.Unlock()
		cancelClaims()
		<-o.claimsDone
	}()
	var scanDone <-chan scanResult
	var scanReply chan OwnerResult
	scanCtx, cancelScan := context.WithCancel(ctx)
	defer func() {
		cancelScan()
		if scanDone != nil {
			<-scanDone
		}
	}()
	events := make(chan mpvplayer.Event, 16)
	var gate imageGate
	var watchCancel context.CancelFunc
	var watchDone chan error
	var watched uint64
	retire := func() {
		if watchCancel != nil {
			watchCancel()
			if err := <-watchDone; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				o.report(err)
			}
			watchCancel = nil
			watchDone = nil
		}
	}
	reconcile := func() error {
		if !o.controller.Running() {
			gate.clear()
			retire()
			return nil
		}
		if watchCancel != nil && watched == o.controller.Generation() {
			return nil
		}
		gate.clear()
		retire()
		session, err := o.player.Session()
		if err != nil {
			return err
		}
		watched = o.controller.Generation()
		watchCtx, cancel := context.WithCancel(ctx)
		watchCancel = cancel
		done := make(chan error, 1)
		watchDone = done
		go func() { done <- session.Watch(watchCtx, events) }()
		current, next := o.controller.MediaPaths()
		gate.reset(ctx, watched, current, next, o.player.PrepareMedia)
		return nil
	}
	var retryTimer *time.Timer
	var retryC <-chan time.Time
	var retryDone <-chan error
	var cancelRetry context.CancelFunc
	var retryPath string
	var retryOffset int64
	var retryGeneration uint64
	var nextRetryAt *time.Time
	lastPlaybackError := ""
	reportPlayback := func(err error) {
		lastPlaybackError = err.Error()
		o.report(err)
	}
	logPlayback := func(event string) {
		current, _ := o.controller.MediaPaths()
		log.Printf("playback %s: channel=%q file=%q running=%v", event, o.controller.SelectedChannelID(), current, o.controller.Running())
	}
	status := func() httpapi.PlaybackStatus {
		channel := o.controller.SelectedChannelID()
		position := o.controller.Snapshot().ChannelState[channel]
		value := httpapi.PlaybackStatus{
			ChannelID: channel, PositionSeconds: position.OffsetSeconds,
			Playback: "stopped", LastError: lastPlaybackError, NextRetryAt: nextRetryAt,
		}
		if position.FilePath != nil {
			value.FilePath = *position.FilePath
		}
		if o.controller.Running() {
			value.Playback = "playing"
		} else if retryC != nil || retryDone != nil {
			value.Playback = "retrying"
		}
		if pending, ok := o.app.pending(); ok {
			value.PendingChannelID = pending.ChannelID
		}
		return value
	}
	clearRetry := func() {
		nextRetryAt = nil
		if retryTimer != nil {
			retryTimer.Stop()
		}
		retryTimer = nil
		retryC = nil
		if retryDone != nil {
			cancelRetry()
			err := <-retryDone
			if err != nil && !errors.Is(err, context.Canceled) {
				o.report(err)
			}
			if err := o.player.Stop(); err != nil {
				o.report(err)
			}
			retryDone = nil
			cancelRetry = nil
		}
	}
	waitForMedia := func() {
		if o.controller.Running() || ctx.Err() != nil {
			return
		}
		clearRetry()
		delay := o.retryDelay
		if delay <= 0 {
			delay = 10 * time.Second
		}
		o.app.showIfNoPreview(filepath.Base(o.controller.SelectedChannelID()) + ": playback unavailable. Retrying...")
		retryTimer = time.NewTimer(delay)
		retryC = retryTimer.C
		when := time.Now().UTC().Add(delay)
		nextRetryAt = &when
		logPlayback("retry scheduled for " + when.Format(time.RFC3339))
	}
	advance := func(generation uint64, usePrepared bool) error {
		if usePrepared && gate.result != nil {
			if gate.result.err != nil {
				o.report(gate.result.err)
			} else if err := o.player.QueuePrepared(gate.result.media); err != nil {
				o.report(err)
			}
		}
		gate.clear()
		retire()
		if err := o.controller.Advance(generation); err != nil {
			reportPlayback(err)
		}
		logPlayback("advanced")
		waitForMedia()
		return reconcile()
	}
	var previewTimer *time.Timer
	var previewC <-chan time.Time
	var previewSequence uint64
	clearTimer := func() {
		if previewTimer != nil {
			previewTimer.Stop()
		}
		previewTimer = nil
		previewC = nil
	}
	defer func() {
		o.app.mu.Lock()
		o.accepting = false
		o.app.mu.Unlock()
		clearRetry()
		clearTimer()
		gate.clear()
		retire()
		result = errors.Join(result, o.app.Stop())
	}()
	if err := o.controller.Start(); err != nil {
		reportPlayback(err)
	}
	logPlayback("startup")
	waitForMedia()
	if err := reconcile(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-retryC:
			nextRetryAt = nil
			retryTimer = nil
			retryC = nil
			retryPath, retryOffset, retryGeneration = o.controller.RetryTarget()
			attemptCtx, cancel := context.WithCancel(ctx)
			cancelRetry = cancel
			done := make(chan error, 1)
			retryDone = done
			path, offset, generation := retryPath, retryOffset, retryGeneration
			go func() { done <- o.player.StartContext(attemptCtx, path, offset, generation) }()
		case err := <-retryDone:
			retryDone = nil
			cancelRetry()
			cancelRetry = nil
			if err != nil {
				reportPlayback(fmt.Errorf("retry %q: %w", retryPath, err))
			} else {
				if err := o.controller.AcceptRetry(retryPath, retryOffset, retryGeneration); err != nil {
					reportPlayback(err)
				}
			}
			if o.controller.Running() {
				logPlayback("recovered")
				o.app.showIfNoPreview("")
			} else {
				waitForMedia()
			}
			if err := reconcile(); err != nil {
				return err
			}
		case scanned := <-scanDone:
			scanDone = nil
			err := scanned.err
			if err == nil && ctx.Err() == nil {
				clearRetry()
				gate.clear()
				clearTimer()
				retire()
				o.app.cancelPreview()
				err = o.controller.ReplaceChannels(scanned.channels)
				previewErr := o.app.replacePreview()
				err = errors.Join(err, previewErr)
			}
			if ctx.Err() != nil {
				err = errors.Join(err, ctx.Err())
			}
			syncErr := reconcile()
			err = errors.Join(err, syncErr)
			if err != nil {
				o.report(fmt.Errorf("rescan: %w", err))
			}
			scanReply <- OwnerResult{State: o.controller.Snapshot(), Running: o.controller.Running(), Generation: o.controller.Generation(), Err: err}
			scanReply = nil
			if syncErr != nil {
				return syncErr
			}
		case <-gate.timerC:
			gate.expire()
			if gate.canAdvance() {
				if err := advance(gate.generation, true); err != nil {
					return err
				}
			}
		case prepared := <-gate.ready:
			gate.accept(prepared)
			if gate.canAdvance() {
				if err := advance(gate.generation, true); err != nil {
					return err
				}
			}
		case <-o.inputWake:
			clearRetry()
			pending, ok := o.app.pending()
			if !ok || pending.Sequence != previewSequence {
				clearTimer()
			}
		case claim := <-o.claimResults:
			pending, ok := o.finishClaim(claim)
			if !ok {
				continue
			}
			clearRetry()
			clearTimer()
			if claim.err != nil {
				lastPlaybackError = claim.err.Error()
				continue
			}
			previewSequence = pending.Sequence
			previewTimer = time.NewTimer(time.Until(pending.Due))
			previewC = previewTimer.C
		case <-previewC:
			pending, ok := o.app.pending()
			clearTimer()
			if !ok || pending.Sequence != previewSequence {
				continue
			}
			if pending.ChannelID != o.controller.SelectedChannelID() || !o.controller.Running() {
				gate.clear()
				retire()
			}
			if err := o.app.CommitPreview(previewSequence, time.Now()); err != nil {
				reportPlayback(err)
			}
			logPlayback("channel committed")
			waitForMedia()
			if err := reconcile(); err != nil {
				return err
			}
		case event := <-events:
			if !o.controller.Running() || event.Generation != o.controller.Generation() {
				continue
			}
			switch event.Kind {
			case "position":
				o.controller.RecordPosition(event.Generation, int64(event.Seconds))
			case "end-file", "process-exit":
				if event.Kind == "process-exit" || event.Reason != "eof" {
					gate.clear()
					clearTimer()
					retire()
					o.app.cancelPreview()
					failure := fmt.Errorf("mpv %s: %s", event.Kind, event.Reason)
					if err := o.controller.Fail(event.Generation, errors.Join(failure, event.Err)); err != nil {
						reportPlayback(err)
					}
					waitForMedia()
					if err := reconcile(); err != nil {
						return err
					}
					continue
				}
				if event.Kind == "end-file" && gate.image {
					continue
				}
				if err := advance(event.Generation, event.Kind == "process-exit"); err != nil {
					return err
				}
			case "ipc-error":
				if event.Err != nil {
					reportPlayback(event.Err)
				}
				// A broken transport is not an EOF and must not silently
				// leave an uncontrollable child or a pending restart behind.
				gate.clear()
				clearTimer()
				retire()
				if err := o.app.Stop(); err != nil {
					o.report(err)
				}
				logPlayback("stopped after IPC loss")
			case "request-error":
				if event.Err != nil {
					o.report(event.Err)
				}
			}
		case request := <-o.requests:
			command := request.command
			var err error
			reason := ""
			switch command.Kind {
			case "status":
			case "rescan":
				if o.scan == nil {
					err = errors.New("rescan unavailable")
				} else if scanDone != nil {
					err = catalog.ErrRescanInProgress
				} else {
					done := make(chan scanResult, 1)
					scanDone = done
					scanReply = request.reply
					go func() {
						channels, err := o.scan(scanCtx)
						done <- scanResult{channels: channels, err: err}
					}()
					continue
				}
			case "volume", "mute", "volume-adjust", "mute-toggle":
				if !o.controller.Running() {
					err = errors.New("player not running")
				} else {
					client := o.player.Client()
					commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
					switch command.Kind {
					case "volume":
						err = client.SetVolume(commandCtx, command.Volume)
					case "mute":
						err = client.SetMute(commandCtx, command.Muted)
					case "volume-adjust":
						if math.IsNaN(command.Volume) || math.IsInf(command.Volume, 0) || command.Volume < -100 || command.Volume > 100 {
							err = errors.New("volume delta must be from -100 to 100")
							break
						}
						var current float64
						current, err = client.Volume(commandCtx)
						if err == nil {
							err = client.SetVolume(commandCtx, math.Max(0, math.Min(100, current+command.Volume)))
						}
					case "mute-toggle":
						var muted bool
						muted, err = client.Muted(commandCtx)
						if err == nil {
							err = client.SetMute(commandCtx, !muted)
						}
					}
					cancel()
				}
			case "start":
				clearRetry()
				err = o.controller.Start()
				if err != nil {
					reportPlayback(err)
				}
				logPlayback("start requested")
				waitForMedia()
			case "stop":
				clearRetry()
				gate.clear()
				clearTimer()
				retire()
				err = o.app.Stop()
				logPlayback("stop requested")
			case "cec":
				resume := cecSelectsPi(command.Message, command.Adapter)
				if resume {
					clearRetry()
				}
				reason, err = o.app.HandleCEC(command.Message, command.Adapter)
				if resume {
					if err != nil {
						reportPlayback(err)
					}
					logPlayback("CEC resume: " + reason)
					waitForMedia()
				} else if reason != "" {
					clearRetry()
					logPlayback("CEC stop: " + reason)
					gate.clear()
					clearTimer()
					retire()
				}
			default:
				err = fmt.Errorf("unknown owner command %q", command.Kind)
			}
			syncErr := reconcile()
			request.reply <- OwnerResult{Status: status(), State: o.controller.Snapshot(), Running: o.controller.Running(), Generation: o.controller.Generation(), Reason: reason, Err: errors.Join(err, syncErr)}
			if syncErr != nil {
				return syncErr
			}
		}
	}
}
