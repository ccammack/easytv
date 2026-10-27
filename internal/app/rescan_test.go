package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
)

func rescanOwner(t *testing.T, real bool, scan func(context.Context) ([]catalog.Channel, error)) (*Owner, context.Context, context.CancelFunc) {
	t.Helper()
	media := filepath.Join(t.TempDir(), "sample.wav")
	ownerWAV(t, media)
	playerConfig := mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}}
	if !real {
		playerConfig.Executable = filepath.Join(t.TempDir(), "missing-mpv")
	}
	p := mpvplayer.New(playerConfig)
	c, err := playback.New([]catalog.Channel{{Path: "/a", Media: []string{media}}, {Path: "/b", Media: []string{media}}}, state.Default(), p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	preview, err := playback.NewPreview(c.ChannelIDs(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := New(c, preview, func() error { return nil }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(c, coordinator, p, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	owner.scan = scan
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	finished := make(chan error, 1)
	go func() { finished <- owner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("rescan worker or owner failed to shut down")
		}
	})
	return owner, ctx, cancel
}

func rescanSubmit(t *testing.T, owner *Owner, ctx context.Context, command OwnerCommand) OwnerResult {
	t.Helper()
	result, err := owner.Submit(ctx, command)
	if err != nil || result.Err != nil {
		t.Fatalf("%s: submit=%v result=%+v", command.Kind, err, result)
	}
	return result
}

func TestRescanOwnerRemainsResponsiveAndRejectsOverlap(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	failure := errors.New("root unreadable")
	owner, ctx, _ := rescanOwner(t, false, func(ctx context.Context) ([]catalog.Channel, error) {
		close(entered)
		select {
		case <-release:
			return nil, failure
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	before := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	done := make(chan OwnerResult, 1)
	go func() {
		result, _ := owner.Submit(ctx, OwnerCommand{Kind: "rescan"})
		done <- result
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	short, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	rescanSubmit(t, owner, short, OwnerCommand{Kind: "status"})
	rescanSubmit(t, owner, short, OwnerCommand{Kind: "channel", Direction: 1})
	result, err := owner.Submit(short, OwnerCommand{Kind: "rescan"})
	if err != nil || !errors.Is(result.Err, catalog.ErrRescanInProgress) {
		t.Fatalf("overlap: %v %+v", err, result)
	}
	close(release)
	select {
	case result := <-done:
		if !errors.Is(result.Err, failure) || result.Generation != before.Generation || result.Running {
			t.Fatalf("failed scan changed playback: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestRescanOwnerJoinsWorkerOnShutdown(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	owner, ctx, cancel := rescanOwner(t, false, func(ctx context.Context) ([]catalog.Channel, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	submitted := make(chan struct{})
	go func() {
		_, _ = owner.Submit(ctx, OwnerCommand{Kind: "rescan"})
		close(submitted)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	select {
	case <-owner.done:
		select {
		case <-exited:
		default:
			t.Fatal("owner finished before scan worker")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner shutdown blocked")
	}
	<-submitted
}

func TestRescanContinuesAfterAcceptedClientDisconnect(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	owner, ctx, _ := rescanOwner(t, false, func(ctx context.Context) ([]catalog.Channel, error) {
		close(entered)
		select {
		case <-release:
			return []catalog.Channel{{Path: "/new", Media: []string{"/new/file.mp4"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	requestCtx, cancel := context.WithCancel(ctx)
	requestDone := make(chan error, 1)
	go func() {
		_, err := owner.Submit(requestCtx, OwnerCommand{Kind: "rescan"})
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	if err := <-requestDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("client cancellation: %v", err)
	}
	close(release)
	for {
		status := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
		if status.State.CurrentChannelID != nil && *status.State.CurrentChannelID == "/new" {
			if status.Running {
				t.Fatal("rescan restarted stopped playback")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
}
