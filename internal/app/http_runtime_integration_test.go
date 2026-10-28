package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
)

func TestRealHTTPControlsKeepCECMonitoringInternal(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	for _, scenario := range []struct {
		name, raw string
		length    uint32
	}{
		{"source away", "4f822000", 4},
		{"TV standby", "089001", 3},
	} {
		t.Run(scenario.name, func(t *testing.T) {
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
			backend := &ownerCECBackend{packets: make(chan cec.ReceivedMessage, 4)}
			config := RuntimeConfig{Roots: []string{root}, StateDir: stateDir, TempDir: tempDir, HTTPListener: listener, Player: mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}}, Show: func(string) {}, Report: func(err error) { t.Errorf("runtime: %v", err) }}
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
			base := "http://" + listener.Addr().String()
			for _, path := range []string{"/adapter", "/events", "/events.ndjson"} {
				response, err := client.Get(base + path)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusNotFound {
					t.Fatalf("%s status=%d", path, response.StatusCode)
				}
			}
			for _, request := range []struct{ path, body string }{
				{"/active-source", ""},
				{"/power", `{"state":"on"}`},
				{"/power", `{"state":"off"}`},
				{"/channel", `{"direction":"next"}`},
			} {
				response, err := client.Post(base+request.path, "application/json", strings.NewReader(request.body))
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusNoContent {
					t.Fatalf("%s status=%d", request.path, response.StatusCode)
				}
			}
			status, err := owner.Submit(ctx, OwnerCommand{Kind: "status"})
			if err != nil || !status.Running {
				t.Fatal("power command manufactured observed standby")
			}
			if backend.claims.Load() != 2 {
				t.Fatal("HTTP Active Source and channel commands must both claim via the service")
			}
			backend.packets <- cec.ReceivedMessage{Raw: scenario.raw, Length: scenario.length}
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
			time.Sleep(2100 * time.Millisecond)
			status, err = owner.Submit(ctx, OwnerCommand{Kind: "status"})
			if err != nil || status.Running {
				t.Fatal("CEC stop did not cancel pending channel preview")
			}
		})
	}
}
