package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/state"
)

func TestRealRescanDoesNotUndoCECStopOrPendingPreview(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	media := filepath.Join(t.TempDir(), "new.wav")
	ownerWAV(t, media)
	owner, ctx, _ := rescanOwner(t, true, func(ctx context.Context) ([]catalog.Channel, error) {
		close(entered)
		select {
		case <-release:
			return []catalog.Channel{{Path: "/new", Media: []string{media}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if !rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"}).Running {
		t.Fatal("not playing")
	}
	finished := make(chan error, 1)
	go func() {
		result, err := owner.Submit(ctx, OwnerCommand{Kind: "rescan"})
		finished <- errors.Join(err, result.Err)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "channel", Direction: 1})
	stopped := rescanSubmit(t, owner, ctx, OwnerCommand{
		Kind: "cec", Message: cec.ReceivedMessage{Raw: "0f36", Length: 2},
		Adapter: cec.AdapterInfo{LogicalAddresses: []int{4}},
	})
	if stopped.Running {
		t.Fatal("CEC failed to stop playback during scan")
	}
	close(release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	time.Sleep(100 * time.Millisecond)
	after := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	if after.Running || after.Generation != stopped.Generation || *after.State.CurrentChannelID != "/new" {
		t.Fatalf("rescan woke playback or retained old catalog: %+v", after)
	}
	rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "channel", Direction: 1})
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for !rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"}).Running {
		select {
		case <-deadline.C:
			t.Fatal("new preview list cannot start playback")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRealHTTPRescanUpdatesPlaybackAndCatalog(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	root, stateDir, tempDir := t.TempDir(), t.TempDir(), t.TempDir()
	add := func(name, file string) string {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, file)
		ownerWAV(t, path)
		return path
	}
	add("a", "first.wav")
	oldFile := add("b", "first.wav")
	add("c", "first.wav")
	selected := filepath.Join(root, "b")
	saved := state.Default()
	saved.CurrentChannelID = &selected
	saved.ChannelState[selected] = state.Position{FilePath: &oldFile}
	if err := state.Save(filepath.Join(stateDir, "state.json"), saved); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	ready, finished := make(chan *Owner, 1), make(chan error, 1)
	config := RuntimeConfig{
		Roots: []string{root}, StateDir: stateDir, TempDir: tempDir, HTTPListener: listener,
		Player:      mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}},
		ClaimSource: func() error { return nil }, Show: func(string) {}, Report: func(error) {},
	}
	go func() { finished <- RunRuntime(ctx, config, ready) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("runtime shutdown blocked")
		}
	}()
	var owner *Owner
	select {
	case owner = <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	client := &http.Client{Timeout: 10 * time.Second}
	post := func(want int) {
		t.Helper()
		response, err := client.Post("http://"+listener.Addr().String()+"/rescan", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != want || want == 204 && len(body) != 0 {
			t.Fatalf("rescan status=%d body=%s err=%v", response.StatusCode, body, err)
		}
	}
	initial := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	newFile := add("b", "new.wav")
	if err := os.Remove(oldFile); err != nil {
		t.Fatal(err)
	}
	add("d", "first.wav")
	post(204)
	after := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	if !after.Running || after.Generation <= initial.Generation || *after.State.ChannelState[selected].FilePath != newFile {
		t.Fatalf("file removal/restart: %+v", after)
	}
	if err := os.Remove(newFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	post(204)
	after = rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	if !after.Running || *after.State.CurrentChannelID != filepath.Join(root, "c") {
		t.Fatalf("did not select next surviving channel: %+v", after)
	}
	cached, err := catalog.LoadCatalog(filepath.Join(stateDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cached.Roots[root].Channels[selected]; ok || len(cached.Roots[root].Channels) != 3 {
		t.Fatalf("catalog not refreshed: %+v", cached)
	}
	catalogPath := filepath.Join(stateDir, "catalog.json")
	backupPath := filepath.Join(stateDir, "catalog.saved")
	if err := os.Rename(catalogPath, backupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(catalogPath, 0700); err != nil {
		t.Fatal(err)
	}
	beforeFailure := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	post(500)
	afterFailure := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	if !afterFailure.Running || afterFailure.Generation != beforeFailure.Generation {
		t.Fatal("catalog save failure interrupted playback")
	}
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backupPath, catalogPath); err != nil {
		t.Fatal(err)
	}
	stopped := rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "stop"})
	post(204)
	after = rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	if after.Running || after.Generation != stopped.Generation {
		t.Fatal("stopped player restarted")
	}
	persisted, err := state.Load(filepath.Join(stateDir, "state.json"))
	if err != nil || *persisted.CurrentChannelID != *after.State.CurrentChannelID {
		t.Fatalf("state=%+v err=%v", persisted, err)
	}
	beforeDisk, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	held := filepath.Join(t.TempDir(), "held")
	if err := os.Rename(root, held); err != nil {
		t.Fatal(err)
	}
	post(500)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	post(500)
	afterDisk, err := os.ReadFile(catalogPath)
	if err != nil || !bytes.Equal(beforeDisk, afterDisk) {
		t.Fatal("failed/empty scan replaced saved catalog")
	}
	after = rescanSubmit(t, owner, ctx, OwnerCommand{Kind: "status"})
	if after.Running || after.Generation != stopped.Generation {
		t.Fatal("failed scan changed player")
	}
}
