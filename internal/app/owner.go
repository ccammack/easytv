package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"sync/atomic"
	"time"
)

// Owner serializes application commands, preview commits and player events.
// After Run begins, its controller, coordinator and player belong only to it.
// This checkpoint handles audio/video only; image policy is not installed.
type Owner struct {
	controller *playback.Controller
	app        *Coordinator
	player     *mpvplayer.Player
	report     func(error)
	requests   chan ownerRequest
	done       chan struct{}
	started    atomic.Bool
}

type OwnerCommand struct {
	Volume    float64
	Muted     bool
	Kind      string // status, start, stop, channel, or cec
	Direction int
	Message   cec.ReceivedMessage
	Adapter   cec.AdapterInfo
}

type OwnerResult struct {
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

func NewOwner(c *playback.Controller, a *Coordinator, p *mpvplayer.Player, report func(error)) (*Owner, error) {
	if c == nil || a == nil || p == nil || report == nil {
		return nil, errors.New("controller, coordinator, player and error reporter required")
	}
	// The coordinator must operate on this exact controller.
	if a.controller != c {
		return nil, errors.New("coordinator/controller mismatch")
	}
	return &Owner{controller: c, app: a, player: p, report: report, requests: make(chan ownerRequest), done: make(chan struct{})}, nil
}

// Submit waits for an owner response. Cancellation after acceptance does not
// roll back the command. A buffered reply lets the owner finish regardless.
func (o *Owner) Submit(ctx context.Context, command OwnerCommand) (OwnerResult, error) {
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
			o.report(err)
		}
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
		clearTimer()
		gate.clear()
		retire()
		result = errors.Join(result, o.app.Stop())
	}()
	if err := o.controller.Start(); err != nil {
		o.report(err)
	}
	if err := reconcile(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
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
		case <-previewC:
			pending, ok := o.app.preview.Pending()
			clearTimer()
			if ok && (pending.ChannelID != o.controller.SelectedChannelID() || !o.controller.Running()) {
				gate.clear()
				retire()
			}
			if err := o.app.CommitPreview(previewSequence, time.Now()); err != nil {
				o.report(err)
			}
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
				if event.Kind == "end-file" && gate.image {
					continue
				}
				if err := advance(event.Generation, event.Kind == "process-exit"); err != nil {
					return err
				}
			case "ipc-error", "request-error":
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
			case "volume", "mute":
				client := o.player.Client()
				if !o.controller.Running() || client == nil {
					err = errors.New("player not running")
				} else {
					commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
					if command.Kind == "volume" {
						err = client.SetVolume(commandCtx, command.Volume)
					} else {
						err = client.SetMute(commandCtx, command.Muted)
					}
					cancel()
				}
			case "start":
				err = o.controller.Start()
			case "stop":
				gate.clear()
				clearTimer()
				retire()
				err = o.app.Stop()
			case "channel":
				var ticket playback.PreviewTicket
				ticket, err = o.app.RequestChannel(command.Direction, time.Now())
				if err == nil {
					clearTimer()
					previewSequence = ticket.Sequence
					previewTimer = time.NewTimer(time.Until(ticket.Due))
					previewC = previewTimer.C
				}
			case "cec":
				reason, err = o.app.HandleCEC(command.Message, command.Adapter)
				if !o.controller.Running() {
					gate.clear()
					clearTimer()
					retire()
				}
			default:
				err = fmt.Errorf("unknown owner command %q", command.Kind)
			}
			syncErr := reconcile()
			request.reply <- OwnerResult{State: o.controller.Snapshot(), Running: o.controller.Running(), Generation: o.controller.Generation(), Reason: reason, Err: errors.Join(err, syncErr)}
			if syncErr != nil {
				return syncErr
			}
		}
	}
}
