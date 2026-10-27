package app

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

var simulatedAdapterFailure = errors.New("simulated adapter loss")

type runtimeCECBackend struct {
	ownerCECBackend
	failed atomic.Bool
}

func (b *runtimeCECBackend) Poll() (*cec.ReceivedMessage, *cec.AdapterEvent, error) {
	if b.failed.Load() {
		return nil, nil, simulatedAdapterFailure
	}
	return b.ownerCECBackend.Poll()
}

func TestRealSupervisedCECRuntimeShutdown(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	for _, adapterFails := range []bool{false, true} {
		name := "context cancellation"
		if adapterFails {
			name = "adapter failure"
		}
		t.Run(name, func(t *testing.T) {
			root, stateDir, tempDir := t.TempDir(), t.TempDir(), t.TempDir()
			channel := filepath.Join(root, "channel")
			if err := os.Mkdir(channel, 0700); err != nil {
				t.Fatal(err)
			}
			media := filepath.Join(channel, "silence.wav")
			ownerWAV(t, media)
			backend := &runtimeCECBackend{ownerCECBackend: ownerCECBackend{packets: make(chan cec.ReceivedMessage, 4)}}
			config := RuntimeConfig{Roots: []string{root}, StateDir: stateDir, TempDir: tempDir, Player: mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}}, Show: func(string) {}, Report: func(error) {}}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			ready := make(chan *Owner, 1)
			finished := make(chan error, 1)
			go func() {
				finished <- RunCECRuntime(ctx, config, backend, "mock-only-no-device", ready, func(cec.AdapterEvent) {})
			}()
			var owner *Owner
			select {
			case owner = <-ready:
			case err := <-finished:
				t.Fatalf("startup: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			status, err := owner.Submit(ctx, OwnerCommand{Kind: "status"})
			if err != nil || status.Err != nil || !status.Running {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			result, err := owner.Submit(ctx, OwnerCommand{Kind: "channel", Direction: 1})
			if err != nil || result.Err != nil {
				t.Fatalf("channel=%+v err=%v", result, err)
			}
			if backend.claims.Load() != 1 {
				t.Fatal("runtime did not route AS through service")
			}
			if adapterFails {
				backend.failed.Store(true)
			} else {
				cancel()
			}
			select {
			case err := <-finished:
				if adapterFails && !errors.Is(err, simulatedAdapterFailure) {
					t.Fatalf("adapter failure lost: %v", err)
				}
				if !adapterFails && err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("supervised runtime failed to terminate")
			}
			if backend.closes.Load() != 1 {
				t.Fatal("adapter backend not closed once")
			}
			saved, err := state.Load(filepath.Join(stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			position := saved.ChannelState[channel]
			if position.FilePath == nil || *position.FilePath != media {
				t.Fatal("shutdown did not persist current media")
			}
			entries, err := os.ReadDir(tempDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("workspace retained: %v err=%v", entries, err)
			}
			t.Log("service AS routing verified; shutdown joined workers, closed backend, persisted playback and removed workspace")
		})
	}
}
