package mpvplayer

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInvalidStart(t *testing.T) {
	p := New(Config{})
	if err := p.Start("", 0, 1); err == nil {
		t.Fatal("empty path accepted")
	}
	if err := p.Start("file.mp4", -1, 1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if p.Running() {
		t.Fatal("invalid start launched child")
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
}
func TestMissingExecutableCleansResources(t *testing.T) {
	p := New(Config{Executable: filepath.Join(t.TempDir(), "missing")})
	if err := p.Start("file.mp4", 0, 1); err == nil {
		t.Fatal("missing executable succeeded")
	}
	if p.cmd != nil || p.client != nil || p.dir != "" || p.log != nil {
		t.Fatal("failed start retained resources")
	}
}
func silentWAV(t *testing.T) string {
	t.Helper()
	var data bytes.Buffer
	data.WriteString("RIFF")
	binary.Write(&data, binary.LittleEndian, uint32(16036))
	data.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		if err := binary.Write(&data, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	data.WriteString("data")
	binary.Write(&data, binary.LittleEndian, uint32(16000))
	data.Write(make([]byte, 16000))
	path := filepath.Join(t.TempDir(), "silence.wav")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestRealPlayerLifecycle(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}})
	defer p.Stop()
	path := silentWAV(t)
	for generation := uint64(1); generation <= 2; generation++ {
		if err := p.Start(path, 0, generation); err != nil {
			t.Fatal(err)
		}
		if !p.Running() || p.Generation() != generation || p.Client() == nil {
			t.Fatal("player not ready")
		}
		if err := p.Start(path, 0, generation); err == nil {
			t.Fatal("second player accepted")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := p.Client().SetVolume(ctx, 25)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		volume, err := p.Client().Volume(ctx)
		cancel()
		if err != nil || volume != 25 {
			t.Fatalf("volume=%v err=%v", volume, err)
		}
		child := p.cmd
		dir := p.dir
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
		if p.Running() || p.Client() != nil || child.ProcessState == nil {
			t.Fatal("child not reaped")
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("temporary directory retained")
		}
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
		t.Logf("real player generation %d: loaded, controlled, stopped, reaped, cleaned", generation)
	}
}
func TestRealPlayerBadMediaCleansResources(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null"}, StartupTimeout: 2 * time.Second})
	defer p.Stop()
	err := p.Start(filepath.Join(t.TempDir(), "missing.wav"), 0, 1)
	if err == nil {
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			t.Fatal("missing media did not terminate mpv")
		}
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
	}
	if p.cmd != nil || p.client != nil || p.dir != "" {
		t.Fatal("failed media retained resources")
	}
}

func TestRealPlayerShortMediaStartup(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null"}})
	defer p.Stop()
	path := silentWAV(t)
	for generation := uint64(1); generation <= 5; generation++ {
		if err := p.Start(path, 0, generation); err != nil {
			t.Fatalf("startup %d: %v", generation, err)
		}
		if p.Client() == nil || p.Generation() != generation {
			t.Fatal("startup did not provide IPC client")
		}
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("five short-file startups completed without file-looping")
}
