package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
)

func TestRecoveryDefaultWaitsTenSeconds(t *testing.T) {
	player := mpvplayer.New(mpvplayer.Config{Executable: filepath.Join(t.TempDir(), "missing-mpv")})
	controller, err := playback.New(
		[]catalog.Channel{{Path: "/Channel One", Media: []string{"/missing.wav"}}},
		state.Default(), player, func(state.State) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := playback.NewPreview(controller.ChannelIDs(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan time.Time, 3)
	coordinator, err := New(controller, preview, func() error { return nil }, func(message string) {
		if strings.Contains(message, "playback unavailable. Retrying...") {
			waiting <- time.Now()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	retried := make(chan time.Time, 3)
	owner, err := NewOwner(controller, coordinator, player, func(err error) {
		if strings.HasPrefix(err.Error(), "retry ") {
			retried <- time.Now()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	done := make(chan error, 1)
	go func() { done <- owner.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	var since time.Time
	select {
	case since = <-waiting:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	short, cancelShort := context.WithTimeout(ctx, time.Second)
	rescanSubmit(t, owner, short, OwnerCommand{Kind: "status"})
	cancelShort()
	select {
	case attempted := <-retried:
		if elapsed := attempted.Sub(since); elapsed < 10*time.Second {
			t.Fatalf("recovery retried too soon: %v", elapsed)
		}
	case <-ctx.Done():
		t.Fatal("default recovery attempt was never made")
	}
}
