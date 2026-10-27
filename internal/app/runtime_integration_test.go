package app

import (
	"context"
	"encoding/binary"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealRuntimeStartupPersistenceRestartAndCleanup(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	root, stateDir, tempDir := t.TempDir(), t.TempDir(), t.TempDir()
	channel := filepath.Join(root, "channel")
	if err := os.Mkdir(channel, 0700); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(channel, "silence.wav")
	ownerWAV(t, media)
	data, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:44], make([]byte, 8*16000)...)
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(data)-44))
	if err := os.WriteFile(media, data, 0600); err != nil {
		t.Fatal(err)
	}
	saved := state.Default()
	saved.CurrentChannelID = &channel
	saved.ChannelState[channel] = state.Position{FilePath: &media, OffsetSeconds: 3}
	if err := state.Save(filepath.Join(stateDir, "state.json"), saved); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(tempDir, "unrelated.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ {
		config := RuntimeConfig{Roots: []string{root}, StateDir: stateDir, TempDir: tempDir, Player: mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}}, ClaimSource: func() error { return nil }, Show: func(string) {}, Report: func(error) {}}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		ready := make(chan *Owner, 1)
		finished := make(chan error, 1)
		go func() { finished <- RunRuntime(ctx, config, ready) }()
		func() {
			defer func() {
				cancel()
				select {
				case err := <-finished:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(5 * time.Second):
					t.Error("runtime failed to shut down")
				}
			}()
			var owner *Owner
			select {
			case owner = <-ready:
			case err := <-finished:
				t.Fatalf("startup: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			submit := func(kind string) OwnerResult {
				t.Helper()
				result, err := owner.Submit(ctx, OwnerCommand{Kind: kind})
				if err != nil {
					t.Fatal(err)
				}
				if result.Err != nil {
					t.Fatal(result.Err)
				}
				return result
			}
			initial := submit("status")
			position := initial.State.ChannelState[channel]
			if !initial.Running || position.FilePath == nil || *position.FilePath != media || position.OffsetSeconds != saved.ChannelState[channel].OffsetSeconds {
				t.Fatalf("restored state=%+v", initial)
			}
			if run == 1 {
				for {
					snapshot := submit("status")
					if snapshot.State.ChannelState[channel].OffsetSeconds != 3 {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(20 * time.Millisecond):
					}
				}
			}
			stopped := submit("stop")
			if stopped.Running {
				t.Fatal("runtime stop failed")
			}
			persisted, err := state.Load(filepath.Join(stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ChannelState[channel].OffsetSeconds != stopped.State.ChannelState[channel].OffsetSeconds {
				t.Fatal("persisted position mismatch")
			}
			saved = persisted
			t.Logf("run=%d restored original media path, stopped and persisted offset=%d", run, saved.ChannelState[channel].OffsetSeconds)
		}()
		entries, err := os.ReadDir(tempDir)
		if err != nil || len(entries) != 1 || entries[0].Name() != "unrelated.txt" {
			t.Fatalf("temporary cleanup: entries=%v err=%v", entries, err)
		}
	}
	cached, err := catalog.LoadCatalog(filepath.Join(stateDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := catalog.ActiveChannels(cached, []string{root})
	if err != nil || len(active) != 1 {
		t.Fatal("runtime catalog not persisted")
	}
	t.Log("isolated catalog reused; runtime temporary directories removed; unrelated file preserved")
}
