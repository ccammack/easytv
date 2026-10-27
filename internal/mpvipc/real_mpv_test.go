package mpvipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Opt in explicitly: EASYTV_TEST_REAL_MPV=1 go test -run TestRealMPVAudio -v ./internal/mpvipc
// Null audio/video outputs ensure this test does not use the TV or speakers.
func TestRealMPVAudio(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("set EASYTV_TEST_REAL_MPV=1 to launch real mpv")
	}
	executable, err := exec.LookPath("mpv")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "etv-mpv-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "ipc.sock")
	media := filepath.Join(dir, "silence.wav")
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, uint32(36+16000))
	wav.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		if err := binary.Write(&wav, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	wav.WriteString("data")
	binary.Write(&wav, binary.LittleEndian, uint32(16000))
	wav.Write(make([]byte, 16000))
	if err := os.WriteFile(media, wav.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := exec.Command(executable, "--no-config", "--no-terminal", "--idle=yes", "--vo=null", "--ao=null", "--loop-file=inf", "--input-ipc-server="+socket)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	defer func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			t.Logf("mpv output:\n%s", output.String())
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var conn net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("mpv exited before IPC: %v", waitErr)
		default:
		}
		conn, err = net.DialTimeout("unix", socket, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("connect mpv IPC: %v", err)
	}
	client := New(conn)
	defer client.Close()
	if _, err := client.Call(ctx, "loadfile", media, "replace"); err != nil {
		t.Fatal(err)
	}
	// Observe a working audio pipeline before changing properties.
	for {
		event, err := client.NextEvent(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(event, []byte(`"file-loaded"`)) {
			break
		}
	}
	for _, want := range []float64{0, 25.5, 100} {
		if err := client.SetVolume(ctx, want); err != nil {
			t.Fatal(err)
		}
		got, err := client.Volume(ctx)
		if err != nil || got != want {
			t.Fatalf("volume want=%v got=%v err=%v", want, got, err)
		}
		t.Logf("real mpv volume set/read: %v", got)
	}
	for _, want := range []bool{true, false} {
		if err := client.SetMute(ctx, want); err != nil {
			t.Fatal(err)
		}
		got, err := client.Muted(ctx)
		if err != nil || got != want {
			t.Fatalf("mute want=%v got=%v err=%v", want, got, err)
		}
		t.Logf("real mpv mute set/read: %v", got)
	}
	// mpv can close the socket before the quit reply reaches the caller.
	_, _ = client.Call(ctx, "quit")
	select {
	case <-done:
		if waitErr != nil {
			t.Fatalf("mpv exit: %v", waitErr)
		}
	case <-ctx.Done():
		t.Fatal("mpv did not exit after quit")
	}
}
