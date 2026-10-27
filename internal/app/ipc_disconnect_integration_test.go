package app

import (
	"context"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealOwnerIPCDisconnectStopsAndRemainsRestartable(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.wav"), filepath.Join(dir, "second.wav")
	ownerWAV(t, first)
	ownerWAV(t, second)
	statePath := filepath.Join(t.TempDir(), "state.json")
	p := mpvplayer.New(mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}})
	defer p.Stop()
	c, err := playback.New([]catalog.Channel{{Path: "/a", Media: []string{first}}, {Path: "/b", Media: []string{second}}}, state.Default(), p, func(s state.State) error { return state.Save(statePath, s) })
	if err != nil {
		t.Fatal(err)
	}
	preview, err := playback.NewPreview([]string{"/a", "/b"}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := New(c, preview, func() error { return nil }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	reports := make(chan error, 16)
	owner, err := NewOwner(c, coordinator, p, func(err error) {
		select {
		case reports <- err:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// Capture the connection before transferring mutable player ownership.
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	oldClient := p.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	finished := make(chan error, 1)
	go func() { finished <- owner.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("owner did not terminate")
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
	initial := submit(OwnerCommand{Kind: "status"})
	if !initial.Running {
		t.Fatal("player not running")
	}
	submit(OwnerCommand{Kind: "channel", Direction: 1})
	// Close only IPC. Do not kill mpv: the owner must perform child cleanup.
	if err := oldClient.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		status := submit(OwnerCommand{Kind: "status"})
		if !status.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("broken IPC left playback marked running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(350 * time.Millisecond)
	stopped := submit(OwnerCommand{Kind: "status"})
	if stopped.Running || stopped.State.CurrentChannelID == nil || *stopped.State.CurrentChannelID != "/a" {
		t.Fatal("broken IPC allowed queued preview or automatic restart")
	}
	saved, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if position := saved.ChannelState["/a"]; position.FilePath == nil || *position.FilePath != first {
		t.Fatal("disconnect did not persist original path")
	}
	select {
	case <-reports:
	default:
		t.Fatal("IPC failure not reported")
	}
	submit(OwnerCommand{Kind: "channel", Direction: 1})
	for {
		status := submit(OwnerCommand{Kind: "status"})
		if status.Running && status.State.CurrentChannelID != nil && *status.State.CurrentChannelID == "/b" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Log("IPC-only disconnect stopped/reaped player, saved position, cancelled preview, stayed stopped, and accepted explicit channel restart")
}
