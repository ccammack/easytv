package mpvplayer

import (
	"context"
	"encoding/json"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealPlayerControllerAdvance(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	first := silentWAV(t)
	second := filepath.Join(filepath.Dir(first), "second.wav")
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, data, 0600); err != nil {
		t.Fatal(err)
	}
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null"}})
	defer p.Stop()
	saves := 0
	c, err := playback.New([]catalog.Channel{{Path: "test", Media: []string{first, second}}}, state.Default(), p, func(state.State) error { saves++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{second, first, second, first} {
		generation := c.Generation()
		oldChild, oldDir := p.cmd, p.dir
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		for {
			raw, err := p.Client().NextEvent(ctx)
			if err != nil {
				cancel()
				t.Fatalf("transition=%d EOF: %v", i, err)
			}
			var event struct {
				Event  string `json:"event"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				cancel()
				t.Fatal(err)
			}
			if event.Event == "end-file" {
				if event.Reason != "eof" {
					cancel()
					t.Fatalf("reason=%q", event.Reason)
				}
				break
			}
		}
		cancel()
		if err := c.Advance(generation); err != nil {
			t.Fatal(err)
		}
		saved := c.Snapshot().ChannelState["test"]
		if saved.FilePath == nil || *saved.FilePath != want || saved.OffsetSeconds != 0 {
			t.Fatalf("wrong advanced position: %+v", saved)
		}
		if oldChild.ProcessState == nil {
			t.Fatal("old child not reaped")
		}
		if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
			t.Fatal("old temporary directory retained")
		}
		current := p.cmd
		if err := c.Advance(generation); err != nil {
			t.Fatal(err)
		}
		if p.cmd != current {
			t.Fatal("duplicate terminal event advanced twice")
		}
		t.Logf("transition=%d advanced and saved; old child reaped; duplicate ignored", i+1)
	}
	if saves != 4 {
		t.Fatalf("saves=%d want=4", saves)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
}
