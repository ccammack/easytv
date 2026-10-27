package mpvplayer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/mpvipc"
	"math"
	"sync"
	"time"
)

// Session is an immutable snapshot, obtained by the serialized player owner.
// A watcher never reads mutable Player fields or changes the controller.
type Session struct {
	client     *mpvipc.Client
	done       <-chan struct{}
	generation uint64
}

type Event struct {
	Generation uint64
	Kind       string
	Seconds    float64
	Reason     string
	Raw        json.RawMessage
	Err        error
}

func (p *Player) Session() (Session, error) {
	if p.cmd == nil || p.client == nil {
		return Session{}, errors.New("no player session")
	}
	return Session{client: p.client, done: p.done, generation: p.generation}, nil
}

// Watch polls time-pos once per second, as the Elixir player does, and drains
// IPC events independently. Only one Watch may consume a session's IPC events.
// The owner cancels and joins this watcher when retiring a session.
// The output is caller-owned and is never closed here.
func (s Session) Watch(ctx context.Context, out chan<- Event) error {
	if s.client == nil || s.done == nil || out == nil {
		return errors.New("session and output channel required")
	}
	ctx, cancel := context.WithCancel(ctx)
	results := make(chan Event)
	var workers sync.WaitGroup
	send := func(event Event) bool {
		event.Generation = s.generation
		select {
		case results <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}
	workers.Add(2)
	go func() {
		defer workers.Done()
		for {
			raw, err := s.client.NextEvent(ctx)
			if err != nil {
				send(Event{Kind: "ipc-closed", Err: err})
				return
			}
			var message struct {
				Event  string `json:"event"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &message); err != nil {
				send(Event{Kind: "ipc-closed", Err: err})
				return
			}
			kind := "ipc-event"
			if message.Event == "end-file" {
				kind = "end-file"
			}
			if !send(Event{Kind: kind, Reason: message.Reason, Raw: raw}) {
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		last := float64(-1)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			requestCtx, done := context.WithTimeout(ctx, 3*time.Second)
			raw, err := s.client.Call(requestCtx, "get_property", "time-pos")
			done()
			if ctx.Err() != nil {
				return
			}
			var seconds *float64
			if err == nil {
				err = json.Unmarshal(raw, &seconds)
				if err == nil && (seconds == nil || math.IsNaN(*seconds) || math.IsInf(*seconds, 0) || *seconds < 0 || *seconds >= float64(math.MaxInt64)) {
					err = fmt.Errorf("invalid time-pos: %s", raw)
				}
			}
			if err != nil {
				if !send(Event{Kind: "request-error", Err: err}) {
					return
				}
			} else if *seconds != last {
				last = *seconds
				if !send(Event{Kind: "position", Seconds: *seconds}) {
					return
				}
			}
		}
	}()
	defer func() { cancel(); workers.Wait() }()
	childDone := s.done
	exited, ipcClosed, ended := false, false, false
	emit := func(event Event) error {
		event.Generation = s.generation
		select {
		case out <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for {
		if exited && ipcClosed {
			return emit(Event{Kind: "process-exit"})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-childDone:
			exited = true
			childDone = nil
		case event := <-results:
			if event.Kind == "ipc-closed" {
				ipcClosed = true
				if !ended {
					event.Kind = "ipc-error"
					if err := emit(event); err != nil {
						return err
					}
				}
				continue
			}
			if event.Kind == "end-file" {
				ended = true
			}
			if err := emit(event); err != nil {
				return err
			}
		}
	}
}
