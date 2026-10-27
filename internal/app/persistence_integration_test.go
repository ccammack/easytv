package app

import (
	"context"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealRuntimePersistsStartupAndChannelBeforeShutdown(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	root, stateDir, tempDir := t.TempDir(), t.TempDir(), t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, dir := range []string{a, b} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		ownerWAV(t, filepath.Join(dir, "sound.wav"))
	}
	config := RuntimeConfig{Roots: []string{root}, StateDir: stateDir, TempDir: tempDir, Scan: true, Player: mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}}, ClaimSource: func() error { return nil }, Show: func(string) {}, Report: func(err error) { t.Logf("runtime diagnostic: %v", err) }}
	for attempt := 0; attempt < 2; attempt++ {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			ready, finished := make(chan *Owner, 1), make(chan error, 1)
			go func() { finished <- RunRuntime(ctx, config, ready) }()
			consumed := false
			defer func() {
				cancel()
				if !consumed {
					select {
					case err := <-finished:
						if err != nil {
							t.Error(err)
						}
					case <-time.After(6 * time.Second):
						t.Error("runtime shutdown did not finish")
					}
				}
			}()
			var owner *Owner
			select {
			case owner = <-ready:
			case err := <-finished:
				consumed = true
				t.Fatalf("startup failed: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			submit := func(command OwnerCommand) OwnerResult {
				t.Helper()
				result, err := owner.Submit(ctx, command)
				if err != nil || result.Err != nil {
					t.Fatalf("submit: %v / %v", err, result.Err)
				}
				return result
			}
			checkDisk := func(want string) {
				t.Helper()
				disk, err := state.Load(filepath.Join(stateDir, "state.json"))
				if err != nil {
					t.Fatal(err)
				}
				pos := disk.ChannelState[want]
				if disk.CurrentChannelID == nil || *disk.CurrentChannelID != want || pos.FilePath == nil || *pos.FilePath != filepath.Join(want, "sound.wav") {
					t.Fatalf("state not persisted before shutdown: %+v", disk)
				}
			}
			status := submit(OwnerCommand{Kind: "status"})
			want := a
			if attempt == 1 {
				want = b
			}
			if !status.Running || status.State.CurrentChannelID == nil || *status.State.CurrentChannelID != want {
				t.Fatal("startup selected wrong channel")
			}
			checkDisk(want)
			if attempt == 0 {
				submit(OwnerCommand{Kind: "channel", Direction: 1})
				for {
					status = submit(OwnerCommand{Kind: "status"})
					if status.Running && status.State.CurrentChannelID != nil && *status.State.CurrentChannelID == b {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(20 * time.Millisecond):
					}
				}
				checkDisk(b)
			}
		}()
		config.Scan = false
	}
	t.Log("real null-output runtime persisted startup and channel target before shutdown, then restored target on next startup")
}
