package app

import (
	"context"
	"encoding/json"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/httpapi"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealHTTPPlaybackControlsUseOwner(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.wav"), filepath.Join(dir, "second.wav")
	ownerWAV(t, first)
	ownerWAV(t, second)
	p := mpvplayer.New(mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}})
	defer p.Stop()
	c, err := playback.New([]catalog.Channel{{Path: "/a", Media: []string{first}}, {Path: "/b", Media: []string{second}}}, state.Default(), p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	preview, err := playback.NewPreview([]string{"/a", "/b"}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int64
	coordinator, err := New(c, preview, func() error { claims.Add(1); return nil }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(c, coordinator, p, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	// Initialize under the test owner, capture the thread-safe IPC client, then
	// transfer controller/player ownership to the application loop.
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	ipc := p.Client()
	server := httpapi.NewServer(nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	finished := make(chan error, 1)
	go func() { finished <- serveOwner(ctx, cancel, owner, server, listener) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("owner/HTTP shutdown blocked")
		}
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	readStatus := func() httpapi.PlaybackStatus {
		t.Helper()
		response, err := client.Get("http://" + listener.Addr().String() + "/status")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status HTTP=%d", response.StatusCode)
		}
		var status httpapi.PlaybackStatus
		if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if status := readStatus(); status.Playback != "playing" || status.ChannelID != "/a" || status.FilePath != first || status.LastError != "" || status.NextRetryAt != nil {
		t.Fatalf("playing status=%+v", status)
	}
	post := func(path, body string) {
		t.Helper()
		response, err := client.Post("http://"+listener.Addr().String()+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 204 {
			t.Fatalf("%s status=%d", path, response.StatusCode)
		}
	}
	post("/volume", `{"volume":25.5}`)
	if value, err := ipc.Volume(ctx); err != nil || value != 25.5 {
		t.Fatalf("volume=%v err=%v", value, err)
	}
	post("/volume/adjust", `{"delta":5}`)
	if value, err := ipc.Volume(ctx); err != nil || value != 30.5 {
		t.Fatalf("adjusted volume=%v err=%v", value, err)
	}
	post("/volume/adjust", `{"delta":100}`)
	if value, err := ipc.Volume(ctx); err != nil || value != 100 {
		t.Fatalf("clamped volume=%v err=%v", value, err)
	}
	post("/volume/adjust", `{"delta":-100}`)
	post("/volume/adjust", `{"delta":-5}`)
	if value, err := ipc.Volume(ctx); err != nil || value != 0 {
		t.Fatalf("clamped volume=%v err=%v", value, err)
	}
	post("/mute", `{"muted":true}`)
	if value, err := ipc.Muted(ctx); err != nil || !value {
		t.Fatalf("mute=%v err=%v", value, err)
	}
	post("/mute", `{"muted":false}`)
	if value, err := ipc.Muted(ctx); err != nil || value {
		t.Fatalf("mute=%v err=%v", value, err)
	}
	post("/mute/toggle", `{}`)
	if value, err := ipc.Muted(ctx); err != nil || !value {
		t.Fatalf("toggled mute=%v err=%v", value, err)
	}
	post("/mute/toggle", `{}`)
	if value, err := ipc.Muted(ctx); err != nil || value {
		t.Fatalf("toggled mute=%v err=%v", value, err)
	}
	post("/channel", `{"direction":"next"}`)
	if status := readStatus(); status.PendingChannelID != "/b" {
		t.Fatalf("pending channel status=%+v", status)
	}
	waitChannelClaims(t, &claims, 1)
	for {
		result, err := owner.Submit(ctx, OwnerCommand{Kind: "status"})
		if err != nil || result.Err != nil {
			t.Fatalf("status=%+v err=%v", result, err)
		}
		if result.State.CurrentChannelID != nil && *result.State.CurrentChannelID == "/b" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if status := readStatus(); status.Playback != "playing" || status.ChannelID != "/b" ||
		status.FilePath != second || status.PendingChannelID != "" {
		t.Fatalf("committed status=%+v", status)
	}
	if result, err := owner.Submit(ctx, OwnerCommand{Kind: "stop"}); err != nil || result.Err != nil {
		t.Fatalf("stop=%+v err=%v", result, err)
	}
	if status := readStatus(); status.Playback != "stopped" || status.NextRetryAt != nil {
		t.Fatalf("stopped status=%+v", status)
	}
	t.Log("volume/mute reached real mpv; channel REST claimed AS and committed; status reflected playing, pending and stopped states")
}
