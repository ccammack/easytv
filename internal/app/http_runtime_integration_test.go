package app

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/httpapi"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealHTTPRuntimePublishesReceivedPacketsOnly(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	root, stateDir, tempDir := t.TempDir(), t.TempDir(), t.TempDir()
	channel := filepath.Join(root, "channel")
	if err := os.Mkdir(channel, 0700); err != nil {
		t.Fatal(err)
	}
	ownerWAV(t, filepath.Join(channel, "silence.wav"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	backend := &ownerCECBackend{packets: make(chan cec.ReceivedMessage, 4)}
	config := RuntimeConfig{Roots: []string{root}, StateDir: stateDir, TempDir: tempDir, HTTPListener: listener, DeviceName: "rpi-tv", Player: mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}}, Show: func(string) {}, Report: func(error) {}}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	ready := make(chan *Owner, 1)
	finished := make(chan error, 1)
	go func() { finished <- RunCECRuntime(ctx, config, backend, "mock-only", ready, func(cec.AdapterEvent) {}) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("runtime did not stop")
		}
	}()
	var owner *Owner
	select {
	case owner = <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	initial, err := owner.Submit(ctx, OwnerCommand{Kind: "status"})
	if err != nil || !initial.Running {
		t.Fatal("runtime not ready")
	}
	client := &http.Client{Timeout: 4 * time.Second}
	base := "http://" + address
	adapter, err := client.Get(base + "/adapter")
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		DeviceName string `json:"device_name"`
		cec.AdapterInfo
	}
	decodeErr := json.NewDecoder(adapter.Body).Decode(&identity)
	adapter.Body.Close()
	if decodeErr != nil || identity.DeviceName != "rpi-tv" || identity.LogicalAddresses[0] != 8 {
		t.Fatal("adapter identity changed")
	}
	sse, err := client.Get(base + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	ndjson, err := client.Get(base + "/events.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer ndjson.Body.Close()
	for _, request := range []struct{ path, body string }{{"/active-source", ""}, {"/power", `{"state":"off"}`}} {
		response, err := client.Post(base+request.path, "application/json", strings.NewReader(request.body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 204 {
			t.Fatalf("%s status=%d", request.path, response.StatusCode)
		}
	}
	status, err := owner.Submit(ctx, OwnerCommand{Kind: "status"})
	if err != nil || !status.Running {
		t.Fatal("power command manufactured observed standby")
	}
	if backend.claims.Load() != 1 {
		t.Fatal("HTTP AS not routed through service")
	}
	scanner := bufio.NewScanner(sse.Body)
	decoder := json.NewDecoder(ndjson.Body)
	for i, raw := range []string{"8f821000", "4f822000"} {
		backend.packets <- cec.ReceivedMessage{Raw: raw, Length: 4}
		if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "id: ") {
			t.Fatal("missing SSE id")
		}
		id := strings.TrimPrefix(scanner.Text(), "id: ")
		if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "data: ") {
			t.Fatal("missing SSE data")
		}
		var event httpapi.CECMessageReceivedEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() || scanner.Text() != "" {
			t.Fatal("missing separator")
		}
		var line httpapi.CECMessageReceivedEvent
		if err := decoder.Decode(&line); err != nil {
			t.Fatal(err)
		}
		if event.Raw != raw || line.Raw != raw || line.ID != id || event.ID != id {
			t.Fatal("receive payload altered")
		}
		if i == 0 && !strings.HasSuffix(id, ":1") {
			t.Fatal("commands injected received events")
		}
	}
	for {
		status, err = owner.Submit(ctx, OwnerCommand{Kind: "status"})
		if err != nil {
			t.Fatal(err)
		}
		if !status.Running {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if rest, err := io.ReadAll(sse.Body); err != nil || len(rest) != 0 {
		t.Fatalf("SSE shutdown: %v %q", err, rest)
	}
	t.Log("HTTP commands used service; only received packets reached both transports; source-away stopped playback; idle SSE closed on shutdown")
}
