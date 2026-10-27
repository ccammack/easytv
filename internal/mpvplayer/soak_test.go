package mpvplayer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func soakSnapshot() string {
	fds, err := os.ReadDir("/proc/self/fd")
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return fmt.Sprintf("goroutines=%d fd=%d fd_error=%v heap_alloc=%d heap_sys=%d", runtime.NumGoroutine(), len(fds), err, m.HeapAlloc, m.HeapSys)
}

func TestRealPlayerSoak(t *testing.T) {
	if os.Getenv("EASYTV_TEST_MPV_SOAK") != "1" {
		t.Skip("opt in with EASYTV_TEST_MPV_SOAK=1")
	}
	cycles := 100
	if text := os.Getenv("EASYTV_MPV_SOAK_CYCLES"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 10000 {
			t.Fatal("EASYTV_MPV_SOAK_CYCLES must be 1..10000")
		}
		cycles = n
	}
	path := silentWAV(t)
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}})
	defer p.Stop()
	started := time.Now()
	commands := 0
	t.Logf("baseline %s", soakSnapshot())
	for i := 1; i <= cycles; i++ {
		if err := p.Start(path, 0, uint64(i)); err != nil {
			t.Fatalf("cycle=%d start: %v", i, err)
		}
		client := p.Client()
		child, dir := p.cmd, p.dir
		if client == nil || !p.Running() || p.Generation() != uint64(i) {
			t.Fatalf("cycle=%d not ready", i)
		}
		for poll := 0; poll < 3; poll++ {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			var seconds *float64
			var err error
			for {
				var raw json.RawMessage
				raw, err = client.Call(ctx, "get_property", "time-pos")
				commands++
				if err == nil {
					seconds = nil
					err = json.Unmarshal(raw, &seconds)
					if err == nil && seconds != nil && *seconds >= 0 {
						break
					}
				}
				if ctx.Err() != nil {
					cancel()
					t.Fatalf("cycle=%d poll=%d position never ready: %v", i, poll, err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
			volume := float64((i + poll) % 101)
			err = client.SetVolume(ctx, volume)
			commands++
			if err == nil {
				var got float64
				got, err = client.Volume(ctx)
				commands++
				if err == nil && got != volume {
					err = fmt.Errorf("got volume %v want %v", got, volume)
				}
			}
			cancel()
			if err != nil {
				t.Fatalf("cycle=%d poll=%d volume: %v", i, poll, err)
			}
			time.Sleep(30 * time.Millisecond)
		}
		if err := p.Stop(); err != nil {
			t.Fatalf("cycle=%d stop: %v", i, err)
		}
		if child.ProcessState == nil || p.cmd != nil || p.Client() != nil || p.Running() {
			t.Fatalf("cycle=%d child not reaped or player state retained", i)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("cycle=%d retained directory=%s err=%v", i, dir, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := client.Call(ctx, "get_property", "time-pos")
		cancel()
		if err == nil {
			t.Fatalf("cycle=%d retired client accepted command", i)
		}
		if i%10 == 0 || i == cycles {
			t.Logf("cycle=%d/%d commands=%d elapsed=%s %s", i, cycles, commands, time.Since(started).Round(time.Millisecond), soakSnapshot())
		}
	}
	runtime.GC()
	t.Logf("final cycles=%d commands=%d elapsed=%s %s", cycles, commands, time.Since(started).Round(time.Millisecond), soakSnapshot())
}

func TestRealPlayerNaturalEOF(t *testing.T) {
	if os.Getenv("EASYTV_TEST_MPV_SOAK") != "1" {
		t.Skip("opt in with EASYTV_TEST_MPV_SOAK=1")
	}
	path := silentWAV(t)
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null"}})
	defer p.Stop()
	for i := 1; i <= 5; i++ {
		if err := p.Start(path, 0, uint64(i)); err != nil {
			t.Fatalf("cycle=%d start: %v", i, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		found := false
		for !found {
			raw, err := p.Client().NextEvent(ctx)
			if err != nil {
				cancel()
				t.Fatalf("cycle=%d wait end-file: %v", i, err)
			}
			var event struct {
				Event  string `json:"event"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				cancel()
				t.Fatal(err)
			}
			if event.Event == "end-file" {
				if event.Reason != "eof" {
					cancel()
					t.Fatalf("cycle=%d unexpected EOF reason=%q", i, event.Reason)
				}
				found = true
			}
		}
		cancel()
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
		t.Logf("natural EOF cycle=%d received eof and cleaned player", i)
	}
}
