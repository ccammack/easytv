package mpvplayer

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"testing"
)

func TestRealPlayerSkipsPreparationFailure(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	good := silentWAV(t)
	bad := filepath.Join(filepath.Dir(good), "bad-image.png")
	attempts := 0
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}, Prepare: func(_ context.Context, path string) (preparedmedia.Result, error) {
		attempts++
		if path == bad {
			return preparedmedia.Result{}, errors.New("deliberate preparation failure")
		}
		return preparedmedia.Result{OriginalPath: path, PlayPath: path}, nil
	}})
	defer p.Stop()
	c, err := playback.New([]catalog.Channel{{Path: "/test", Media: []string{bad, good}}}, state.Default(), p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	position := c.Snapshot().ChannelState["/test"]
	if attempts != 2 || position.FilePath == nil || *position.FilePath != good || !p.Running() {
		t.Fatalf("attempts=%d state=%+v", attempts, position)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	t.Log("failed image preparation skipped; following audio item started and cleaned")
}
