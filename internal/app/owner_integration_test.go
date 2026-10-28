package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func ownerWAV(t *testing.T, path string) {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(16036))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(16000))
	b.Write(make([]byte, 16000))
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRealOwnerSerializesEOFPreviewAndStop(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	dir := t.TempDir()
	first, second, other := filepath.Join(dir, "first.wav"), filepath.Join(dir, "second.wav"), filepath.Join(dir, "other.wav")
	for _, path := range []string{first, second, other} {
		ownerWAV(t, path)
	}
	p := mpvplayer.New(mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null"}})
	var claims, saves atomic.Int64
	c, err := playback.New([]catalog.Channel{{Path: "/a", Media: []string{first, second}}, {Path: "/b", Media: []string{other}}}, state.Default(), p, func(state.State) error { saves.Add(1); return nil })
	if err != nil {
		t.Fatal(err)
	}
	preview, err := playback.NewPreview([]string{"/a", "/b"}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(c, preview, func() error { claims.Add(1); return nil }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	reported := make(chan error, 100)
	o, err := NewOwner(c, a, p, func(err error) {
		select {
		case reported <- err:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
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
			t.Error("owner did not shut down")
		}
		if p.Running() || p.Client() != nil {
			t.Error("shutdown retained player")
		}
	}()
	submit := func(command OwnerCommand) OwnerResult {
		t.Helper()
		result, err := o.Submit(ctx, command)
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
		t.Fatal("owner did not start playback")
	}
	for {
		result := submit(OwnerCommand{Kind: "status"})
		position := result.State.ChannelState["/a"]
		if position.FilePath != nil && *position.FilePath == second {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Log("owner advanced first to second file on real EOF")
	submit(OwnerCommand{Kind: "channel", Direction: 1})
	waitChannelClaims(t, &claims, 1)
	for {
		result := submit(OwnerCommand{Kind: "status"})
		if result.State.CurrentChannelID != nil && *result.State.CurrentChannelID == "/b" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Log("owner committed preview and installed replacement session")
	submit(OwnerCommand{Kind: "channel", Direction: 1})
	waitChannelClaims(t, &claims, 2)
	stopped := submit(OwnerCommand{Kind: "stop"})
	if stopped.Running {
		t.Fatal("stop left running playback")
	}
	time.Sleep(350 * time.Millisecond)
	after := submit(OwnerCommand{Kind: "status"})
	if after.Running || after.Generation != stopped.Generation {
		t.Fatal("cancelled preview or stale event restarted playback")
	}
	if claims.Load() != 2 || saves.Load() < 1 {
		t.Fatal("missing claims or persistence")
	}
	t.Log("stop cancelled preview, joined watcher, and prevented delayed restart")
}

func TestOwnerRejectsInvalidConstruction(t *testing.T) {
	if _, err := NewOwner(nil, nil, nil, nil); err == nil {
		t.Fatal("invalid owner accepted")
	}
}
