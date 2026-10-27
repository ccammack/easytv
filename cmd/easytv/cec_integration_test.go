package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type cliCECBackend struct{ opens, closes atomic.Int64 }

func (b *cliCECBackend) Open(string) error        { b.opens.Add(1); return nil }
func (b *cliCECBackend) Close() error             { b.closes.Add(1); return nil }
func (b *cliCECBackend) SetPower(bool) error      { return nil }
func (b *cliCECBackend) ClaimActiveSource() error { return nil }
func (b *cliCECBackend) SelfID() (int, error)     { return 8, nil }
func (b *cliCECBackend) AdapterInfo() (cec.AdapterInfo, error) {
	physical := "1.0.0.0"
	return cec.AdapterInfo{LogicalAddresses: []int{8}, PhysicalAddress: &physical}, nil
}
func (b *cliCECBackend) Poll() (*cec.ReceivedMessage, *cec.AdapterEvent, error) {
	time.Sleep(time.Millisecond)
	return nil, nil, nil
}

func TestRealCLIWithInjectedCECBackend(t *testing.T) {
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
	backend := &cliCECBackend{}
	factory := func(device string) cec.Backend {
		if device != "mock-only-no-device" {
			t.Fatal(device)
		}
		return backend
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := runWithBackendFactory(ctx, []string{"--headless", "--cec-device", "mock-only-no-device", "--state-dir", stateDir, "--temp-dir", tempDir, "--run-for", "2s", root}, &stdout, &stderr, factory)
	if err != nil {
		t.Fatalf("CLI: %v diagnostics=%s", err, stderr.String())
	}
	if backend.opens.Load() != 1 || backend.closes.Load() != 1 {
		t.Fatal("CEC backend lifecycle mismatch")
	}
	if !strings.Contains(stdout.String(), "runtime ready; running=true") || !strings.Contains(stdout.String(), "runtime stopped") {
		t.Fatal(stdout.String())
	}
	saved, err := state.Load(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	position := saved.ChannelState[channel]
	if position.FilePath == nil || *position.FilePath != media {
		t.Fatal("media state not persisted")
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil || len(entries) != 0 {
		t.Fatal("runtime workspace retained")
	}
	t.Log("CLI selected supervised CEC runtime using injected mock backend; private playback and bounded shutdown passed")
}
