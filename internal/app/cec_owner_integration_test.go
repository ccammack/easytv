package app

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// This backend never opens a device or transmits a real CEC message.
type ownerCECBackend struct {
	packets chan cec.ReceivedMessage
	claims  atomic.Int64
	closes  atomic.Int64
}

func (b *ownerCECBackend) Open(string) error        { return nil }
func (b *ownerCECBackend) Close() error             { b.closes.Add(1); return nil }
func (b *ownerCECBackend) SetPower(bool) error      { return nil }
func (b *ownerCECBackend) ClaimActiveSource() error { b.claims.Add(1); return nil }
func (b *ownerCECBackend) SelfID() (int, error)     { return 8, nil }
func (b *ownerCECBackend) AdapterInfo() (cec.AdapterInfo, error) {
	physical := "1.0.0.0"
	return cec.AdapterInfo{LogicalAddresses: []int{8}, PhysicalAddress: &physical}, nil
}
func (b *ownerCECBackend) Poll() (*cec.ReceivedMessage, *cec.AdapterEvent, error) {
	select {
	case packet := <-b.packets:
		return &packet, nil, nil
	case <-time.After(time.Millisecond):
		return nil, nil, nil
	}
}

type observedCECSubmitter struct {
	owner   *Owner
	handled chan OwnerResult
}

func (s *observedCECSubmitter) Submit(ctx context.Context, command OwnerCommand) (OwnerResult, error) {
	result, err := s.owner.Submit(ctx, command)
	if err == nil {
		s.handled <- result
	}
	return result, err
}

func TestRealCECServiceStopsOwnerAndCancelsPreview(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	for _, scenario := range []struct {
		name, raw string
		length    uint32
	}{
		{"other active source", "4f822000", 4},
		{"TV standby report", "089001", 3},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			first, second := filepath.Join(dir, "a.wav"), filepath.Join(dir, "b.wav")
			ownerWAV(t, first)
			ownerWAV(t, second)
			statePath := filepath.Join(t.TempDir(), "state.json")
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			backend := &ownerCECBackend{packets: make(chan cec.ReceivedMessage, 4)}
			service, err := cec.NewService(backend, "mock-only-no-device")
			if err != nil {
				t.Fatal(err)
			}
			serviceDone := make(chan error, 1)
			go func() { serviceDone <- service.Run(ctx) }()
			select {
			case <-service.Ready():
			case <-ctx.Done():
				cancel()
				t.Fatal(ctx.Err())
			}
			p := mpvplayer.New(mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}})
			c, err := playback.New([]catalog.Channel{{Path: "/a", Media: []string{first}}, {Path: "/b", Media: []string{second}}}, state.Default(), p, func(s state.State) error { return state.Save(statePath, s) })
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			preview, err := playback.NewPreview([]string{"/a", "/b"}, 200*time.Millisecond)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			coordinator, err := New(c, preview, func() error { return service.ClaimActiveSource(ctx) }, func(string) {})
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			owner, err := NewOwner(c, coordinator, p, func(error) {})
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			ownerDone := make(chan error, 1)
			go func() { ownerDone <- owner.Run(ctx) }()
			observer := &observedCECSubmitter{owner: owner, handled: make(chan OwnerResult, 4)}
			forwardDone := make(chan error, 1)
			go func() { forwardDone <- ForwardCEC(ctx, service.Observations(), observer, func(error) {}) }()
			defer func() {
				cancel()
				for name, done := range map[string]<-chan error{"owner": ownerDone, "forwarder": forwardDone, "CEC service": serviceDone} {
					select {
					case err := <-done:
						if err != nil && !errors.Is(err, context.Canceled) {
							t.Errorf("%s: %v", name, err)
						}
					case <-time.After(5 * time.Second):
						t.Errorf("%s did not stop", name)
					}
				}
				if p.Running() || p.Client() != nil {
					t.Error("player retained after shutdown")
				}
				if backend.closes.Load() != 1 {
					t.Error("backend not closed exactly once")
				}
			}()
			submit := func(command OwnerCommand) OwnerResult {
				t.Helper()
				result, err := owner.Submit(ctx, command)
				if err != nil {
					t.Fatal(err)
				}
				if result.Err != nil {
					t.Fatal(result.Err)
				}
				return result
			}
			receive := func() OwnerResult {
				t.Helper()
				select {
				case result := <-observer.handled:
					if result.Err != nil {
						t.Fatal(result.Err)
					}
					return result
				case <-ctx.Done():
					t.Fatal(ctx.Err())
					return OwnerResult{}
				}
			}
			initial := submit(OwnerCommand{Kind: "status"})
			if !initial.Running {
				t.Fatal("initial player not running")
			}
			backend.packets <- cec.ReceivedMessage{Raw: "8f821000", Length: 4}
			self := receive()
			if !self.Running || self.Reason != "" {
				t.Fatal("own Active Source stopped playback")
			}
			submit(OwnerCommand{Kind: "channel", Direction: 1})
			waitChannelClaims(t, &backend.claims, 1)
			backend.packets <- cec.ReceivedMessage{Raw: scenario.raw, Length: scenario.length}
			stopped := receive()
			if stopped.Running || stopped.Reason == "" {
				t.Fatalf("CEC did not stop owner: %+v", stopped)
			}
			time.Sleep(350 * time.Millisecond)
			later := submit(OwnerCommand{Kind: "status"})
			if later.Running || later.Generation != stopped.Generation || backend.claims.Load() != 1 {
				t.Fatal("pending preview or old event restarted playback/reclaimed AS")
			}
			persisted, err := state.Load(statePath)
			if err != nil {
				t.Fatal(err)
			}
			position := persisted.ChannelState["/a"]
			if position.FilePath == nil || *position.FilePath != first {
				t.Fatal("stop did not persist current original media path")
			}
			t.Logf("packet=%s reason=%s stopped player, saved original path, cancelled preview, and made no extra AS claim", scenario.raw, stopped.Reason)
		})
	}
}
