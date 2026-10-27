package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("EASYTV_CLI_HELPER") != "1" {
		return
	}
	index := 0
	for index < len(os.Args) && os.Args[index] != "--" {
		index++
	}
	if index == len(os.Args) {
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[index+1:], os.Stdout, os.Stderr)
	cancel()
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func TestRealCLIHandlesSIGTERMAndPersistsState(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_CLI") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_CLI=1")
	}
	root, stateDir, tempDir := t.TempDir(), t.TempDir(), t.TempDir()
	channel := filepath.Join(root, "channel")
	if err := os.Mkdir(channel, 0700); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(channel, "silence.wav")
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, uint32(36+8*16000))
	wav.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		if err := binary.Write(&wav, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	wav.WriteString("data")
	binary.Write(&wav, binary.LittleEndian, uint32(8*16000))
	wav.Write(make([]byte, 8*16000))
	if err := os.WriteFile(media, wav.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCLIHelperProcess$", "--", "--headless", "--state-dir", stateDir, "--temp-dir", tempDir, root)
	cmd.Env = append(os.Environ(), "EASYTV_CLI_HELPER=1")
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			line <- scanner.Text()
		} else {
			line <- ""
		}
	}()
	select {
	case text := <-line:
		if !strings.Contains(text, "runtime ready; running=true") {
			t.Fatalf("unexpected readiness: %q", text)
		}
	case <-ctx.Done():
		t.Fatal("CLI startup timed out")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatalf("SIGTERM shutdown: %v diagnostics=%s", err, diagnostics.String())
	}
	saved, err := state.Load(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	position := saved.ChannelState[channel]
	if saved.CurrentChannelID == nil || *saved.CurrentChannelID != channel || position.FilePath == nil || *position.FilePath != media {
		t.Fatalf("saved=%+v", saved)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("runtime temp files retained: %v err=%v", entries, err)
	}
	t.Log("CLI accepted SIGTERM, exited successfully, persisted original media state and cleaned runtime workspace")
}
