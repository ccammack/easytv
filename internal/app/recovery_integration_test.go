package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
)

func TestRealMediaRecoveryKeepsChannelAndCancelsOnUserOrCEC(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	for _, action := range []string{"reconnect", "CEC standby", "CEC source away", "channel change", "stop", "rescan", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			current := filepath.Join(dir, "current.wav")
			backup := filepath.Join(dir, "disconnected.wav")
			other := filepath.Join(dir, "other.wav")
			ownerWAV(t, current)
			ownerWAV(t, other)
			p := mpvplayer.New(mpvplayer.Config{
				OutputArgs:     []string{"--ao=null", "--vo=null", "--loop-file=inf"},
				StartupTimeout: time.Second,
			})
			statePath := filepath.Join(t.TempDir(), "state.json")
			channels := []catalog.Channel{
				{Path: "/Channel One", Media: []string{current, other}},
				{Path: "/Other", Media: []string{other}},
			}
			c, err := playback.New(channels, state.Default(), p, func(s state.State) error { return state.Save(statePath, s) })
			if err != nil {
				t.Fatal(err)
			}
			preview, err := playback.NewPreview(c.ChannelIDs(), 10*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			messages := make(chan string, 100)
			var claims atomic.Int64
			coordinator, err := New(c, preview, func() error { claims.Add(1); return nil }, func(message string) { messages <- message })
			if err != nil {
				t.Fatal(err)
			}
			reported := make(chan error, 100)
			owner, err := NewOwner(c, coordinator, p, func(err error) { reported <- err })
			if err != nil {
				t.Fatal(err)
			}
			owner.retryDelay = 30 * time.Millisecond
			owner.scan = func(context.Context) ([]catalog.Channel, error) { return channels, nil }
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			done := make(chan error, 1)
			go func() { done <- owner.Run(ctx) }()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(4 * time.Second):
					t.Error("recovery worker not joined")
				}
				if p.Running() || p.Client() != nil {
					t.Error("player retained after shutdown")
				}
			}()
			rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
			// The IPC client is thread-safe; capture it before disconnecting.
			ipc := p.Client()
			if err := os.Rename(current, backup); err != nil {
				t.Fatal(err)
			}
			// A FIFO makes mpv's load block, so controls must stay responsive
			// while a recovery attempt is actually in progress.
			if err := syscall.Mkfifo(current, 0600); err != nil {
				t.Fatal(err)
			}
			_, _ = ipc.Call(ctx, "stop")
			for {
				select {
				case message := <-messages:
					if message == "Channel One: playback unavailable. Retrying..." {
						goto waiting
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
		waiting:
			time.Sleep(100 * time.Millisecond)
			short, stopShort := context.WithTimeout(ctx, 300*time.Millisecond)
			status := rescanSubmit(t, owner, short, OwnerCommand{Kind: "status"})
			stopShort()
			if status.Running || *status.State.CurrentChannelID != "/Channel One" {
				t.Fatalf("recovery changed channel or skipped missing file: %+v", status)
			}
			if claims.Load() != 0 {
				t.Fatal("recovery claimed active source")
			}
			switch action {
			case "CEC standby":
				rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "cec",
					Message: cec.ReceivedMessage{Raw: "0f36", Length: 2},
					Adapter: cec.AdapterInfo{LogicalAddresses: []int{4}},
				})
			case "CEC source away":
				physical := "1.0.0.0"
				rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "cec",
					Message: cec.ReceivedMessage{Raw: "8f822000", Length: 4},
					Adapter: cec.AdapterInfo{LogicalAddresses: []int{4}, PhysicalAddress: &physical},
				})
			case "channel change":
				rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "channel", Direction: 1})
			case "stop":
				rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "stop"})
			case "rescan":
				rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "rescan"})
			case "shutdown":
				cancel()
				return
			}
			if err := os.Remove(current); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, current); err != nil {
				t.Fatal(err)
			}
			if action == "reconnect" || action == "channel change" {
				for {
					status = rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
					if status.Running {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(10 * time.Millisecond):
					}
				}
				want := "/Channel One"
				if action == "channel change" {
					want = "/Other"
				}
				if *status.State.CurrentChannelID != want {
					t.Fatalf("unexpected recovered channel: %+v", status)
				}
			} else {
				time.Sleep(150 * time.Millisecond)
				status = rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
				if status.Running {
					t.Fatal("canceled recovery restarted playback")
				}
			}
			persisted, err := state.Load(statePath)
			if err != nil || persisted.CurrentChannelID == nil {
				t.Fatalf("saved state lost: %+v %v", persisted, err)
			}
		})
	}
}

func TestRealMissingMediaRetryDoesNotSkip(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	dir := t.TempDir()
	missing, good := filepath.Join(dir, "missing.wav"), filepath.Join(dir, "good.wav")
	ownerWAV(t, good)
	saved := state.Default()
	id := "/Channel One"
	saved.CurrentChannelID = &id
	saved.ChannelState[id] = state.Position{FilePath: &missing, OffsetSeconds: 17}
	p := mpvplayer.New(mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}})
	// Begin stopped recovery without normal startup candidate fallback.
	c, err := playback.New([]catalog.Channel{{Path: id, Media: []string{missing, good}}}, saved, p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		path, offset, gen := c.RetryTarget()
		if path != missing || offset != 17 {
			t.Fatalf("retry skipped selected file: %s %d", path, offset)
		}
		err := p.StartContext(context.Background(), path, offset, gen)
		if err == nil || !strings.Contains(err.Error(), "mpv") {
			t.Fatalf("missing media result: %v", err)
		}
		if c.Running() || p.Running() || p.Client() != nil {
			t.Fatal("missing media retained player")
		}
	}
}
