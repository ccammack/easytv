package app

import (
	"context"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"github.com/ccammack/easytv/internal/state"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealOwnerImageTimerWaitsForPreparation(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_IMAGES") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_IMAGES=1")
	}
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.png"), filepath.Join(dir, "second.png")
	for _, path := range []string{first, second} {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := png.Encode(file, image.NewNRGBA(image.Rect(0, 0, 64, 32)))
		closeErr := file.Close()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	preparer, err := preparedmedia.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer preparer.Close()
	release := make(chan struct{})
	p := mpvplayer.New(mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null"}, Prepare: func(ctx context.Context, path string) (preparedmedia.Result, error) {
		if path == second {
			select {
			case <-release:
			case <-ctx.Done():
				return preparedmedia.Result{}, ctx.Err()
			}
		}
		return preparer.Prepare(ctx, path)
	}})
	c, err := playback.New([]catalog.Channel{{Path: "/images", Media: []string{first, second}}}, state.Default(), p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	preview, err := playback.NewPreview([]string{"/images"}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := New(c, preview, func() error { return nil }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewOwner(c, coordinator, p, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	finished := make(chan error, 1)
	go func() { finished <- o.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("owner did not stop")
		}
		if p.Running() {
			t.Error("player retained")
		}
	}()
	status := func() OwnerResult {
		t.Helper()
		result, err := o.Submit(ctx, OwnerCommand{Kind: "status"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		return result
	}
	initial := status()
	if !initial.Running {
		t.Fatal("image not started")
	}
	time.Sleep(imageDisplayDuration + 200*time.Millisecond)
	waiting := status()
	if waiting.Generation != initial.Generation || !waiting.Running {
		t.Fatal("advanced while next-image preparation was blocked")
	}
	t.Log("ten-second display elapsed; owner remained on current image while preparation blocked")
	close(release)
	for {
		result := status()
		position := result.State.ChannelState["/images"]
		if position.FilePath != nil && *position.FilePath == second {
			if position.OffsetSeconds != 0 || result.Generation == initial.Generation {
				t.Fatal("wrong image transition state")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Log("prepared next image consumed after timer gate; original filename and zero offset retained")
	stopped, err := o.Submit(ctx, OwnerCommand{Kind: "stop"})
	if err != nil || stopped.Err != nil {
		t.Fatalf("stop=%v result=%v", err, stopped.Err)
	}
	if stopped.Running {
		t.Fatal("image stop failed")
	}
}
