package mpvplayer

import (
	"context"
	"encoding/binary"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"testing"
	"time"
)

func TestRealPlayerSessionPollingAndAdvance(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	path := silentWAV(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:44], make([]byte, 8*16000)...)
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(data)-44))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	saved := state.Default()
	saved.ChannelState["test"] = state.Position{FilePath: &path, OffsetSeconds: 3}
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null"}})
	defer p.Stop()
	c, err := playback.New([]catalog.Channel{{Path: "test", Media: []string{path}}}, saved, p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	session, err := p.Session()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	out := make(chan Event)
	finished := make(chan error, 1)
	go func() { finished <- session.Watch(ctx, out) }()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("watcher did not finish")
		}
	}()
	positions, terminals := 0, 0
	generation := c.Generation()
	for terminals < 2 {
		select {
		case event := <-out:
			if event.Generation != generation {
				t.Fatal("wrong session generation")
			}
			switch event.Kind {
			case "position":
				if event.Generation != c.Generation() {
					before := c.Snapshot().ChannelState["test"].OffsetSeconds
					c.RecordPosition(event.Generation, int64(event.Seconds))
					if c.Snapshot().ChannelState["test"].OffsetSeconds != before {
						t.Fatal("retired position changed replacement")
					}
					continue
				}
				positions++
				c.RecordPosition(event.Generation, int64(event.Seconds))
				got := c.Snapshot().ChannelState["test"].OffsetSeconds
				want := int64(event.Seconds) - 3
				if want < 0 {
					want = 0
				}
				if got != want {
					t.Fatalf("resume offset=%d want=%d", got, want)
				}
				t.Logf("one-second poll position=%.3f resume_offset=%d", event.Seconds, got)
			case "end-file", "process-exit":
				if event.Kind == "end-file" && event.Reason != "eof" {
					t.Fatalf("reason=%q", event.Reason)
				}
				before := c.Generation()
				if err := c.Advance(event.Generation); err != nil {
					t.Fatal(err)
				}
				if terminals == 0 && c.Generation() == before {
					t.Fatal("first terminal did not advance")
				}
				if terminals == 1 && c.Generation() != before {
					t.Fatal("duplicate terminal advanced again")
				}
				terminals++
				t.Logf("terminal=%s generation=%d handled", event.Kind, event.Generation)
			case "ipc-error":
				t.Fatalf("unexpected IPC error: %v", event.Err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if positions < 1 {
		t.Fatal("no position polls received")
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
}
