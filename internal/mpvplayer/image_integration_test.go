package mpvplayer

import (
	"context"
	"encoding/json"
	"github.com/ccammack/easytv/internal/catalog"
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

func TestRealPreparedImagePlayback(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_IMAGES") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_IMAGES=1")
	}
	original := filepath.Join(t.TempDir(), "original.PNG")
	file, err := os.Create(original)
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
	preparer, err := preparedmedia.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer preparer.Close()
	var prepared preparedmedia.Result
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null"}, Prepare: func(ctx context.Context, path string) (preparedmedia.Result, error) {
		result, err := preparer.Prepare(ctx, path)
		prepared = result
		return result, err
	}})
	defer p.Stop()
	saved := state.Default()
	saved.ChannelState["/images"] = state.Position{FilePath: &original, OffsetSeconds: 90}
	var persisted state.State
	c, err := playback.New([]catalog.Channel{{Path: "/images", Media: []string{original}}}, saved, p, func(s state.State) error { persisted = s; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if prepared.PlayPath == original || !prepared.Temporary {
		t.Fatal("image did not use prepared copy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		raw, err := p.Client().Call(ctx, "get_property", "path")
		var path string
		if err == nil && json.Unmarshal(raw, &path) == nil && path == prepared.PlayPath {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("prepared image never loaded: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, err := p.Client().Call(ctx, "get_property", "options/image-display-duration")
	var duration float64
	if err != nil || json.Unmarshal(raw, &duration) != nil || duration != 86400 {
		t.Fatalf("image duration=%s err=%v", raw, err)
	}
	time.Sleep(1200 * time.Millisecond)
	if !p.Running() {
		t.Fatal("image player exited before owner display timer")
	}
	c.RecordPosition(c.Generation(), 90)
	child := p.cmd
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	position := persisted.ChannelState["/images"]
	if position.FilePath == nil || *position.FilePath != original || position.OffsetSeconds != 0 {
		t.Fatalf("persisted=%+v", position)
	}
	if child.ProcessState == nil || p.Client() != nil {
		t.Fatal("image child not reaped")
	}
	t.Log("prepared copy loaded; image held open; persisted original path at zero offset; child reaped")
}
