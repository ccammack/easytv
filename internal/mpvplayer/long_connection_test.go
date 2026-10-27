package mpvplayer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealPlayerLongConnection(t *testing.T) {
	if os.Getenv("EASYTV_TEST_MPV_LONG") != "1" {
		t.Skip("opt in with EASYTV_TEST_MPV_LONG=1")
	}
	duration := 120 * time.Second
	if text := os.Getenv("EASYTV_MPV_LONG_DURATION"); text != "" {
		var err error
		duration, err = time.ParseDuration(text)
		if err != nil || duration < 10*time.Second || duration > 24*time.Hour {
			t.Fatal("duration must be 10s..24h")
		}
	}
	p := New(Config{OutputArgs: []string{"--ao=null", "--vo=null", "--loop-file=inf"}})
	defer p.Stop()
	if err := p.Start(silentWAV(t), 0, 1); err != nil {
		t.Fatal(err)
	}
	client := p.Client()
	child, dir := p.cmd, p.dir
	ctx, cancel := context.WithCancel(context.Background())
	var total, position, volume atomic.Uint64
	eventErr := make(chan error, 1)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			raw, err := client.NextEvent(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					eventErr <- err
				}
				return
			}
			var event struct {
				Event string `json:"event"`
				Name  string `json:"name"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				eventErr <- err
				return
			}
			total.Add(1)
			if event.Event == "property-change" {
				switch event.Name {
				case "time-pos":
					position.Add(1)
				case "volume":
					volume.Add(1)
				}
			}
		}
	}()
	defer func() { cancel(); <-drained }()
	for id, name := range []string{"time-pos", "volume"} {
		requestCtx, done := context.WithTimeout(ctx, 3*time.Second)
		_, err := client.Call(requestCtx, "observe_property", id+1, name)
		done()
		if err != nil {
			t.Fatal(err)
		}
	}
	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readyCancel()
	for {
		raw, err := client.Call(readyCtx, "get_property", "time-pos")
		var seconds *float64
		if err == nil && json.Unmarshal(raw, &seconds) == nil && seconds != nil {
			break
		}
		if readyCtx.Err() != nil {
			t.Fatalf("media never ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("baseline %s", soakSnapshot())
	started := time.Now()
	deadline := started.Add(duration)
	nextLog := started.Add(10 * time.Second)
	rounds, commands := 0, 0
	var maxRound time.Duration
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		<-ticker.C
		select {
		case err := <-eventErr:
			t.Fatalf("event reader failed: %v", err)
		default:
		}
		roundStarted := time.Now()
		value := float64(rounds % 101)
		requestCtx, done := context.WithTimeout(ctx, 3*time.Second)
		if err := client.SetVolume(requestCtx, value); err != nil {
			done()
			t.Fatalf("round=%d set volume: %v", rounds, err)
		}
		commands++
		replies := make(chan error, 2)
		go func() {
			got, err := client.Volume(requestCtx)
			if err == nil && got != value {
				err = fmt.Errorf("volume reply=%v want=%v", got, value)
			}
			replies <- err
		}()
		go func() {
			raw, err := client.Call(requestCtx, "get_property", "time-pos")
			var seconds *float64
			if err == nil {
				err = json.Unmarshal(raw, &seconds)
				if err == nil && (seconds == nil || *seconds < 0) {
					err = fmt.Errorf("invalid position reply: %s", raw)
				}
			}
			replies <- err
		}()
		first, second := <-replies, <-replies
		done()
		commands += 2
		if first != nil || second != nil {
			t.Fatalf("round=%d reply errors: %v / %v", rounds, first, second)
		}
		rounds++
		if elapsed := time.Since(roundStarted); elapsed > maxRound {
			maxRound = elapsed
		}
		if !p.Running() {
			t.Fatal("owned player exited unexpectedly")
		}
		if time.Now().After(nextLog) {
			t.Logf("elapsed=%s rounds=%d commands=%d events=%d position_events=%d volume_events=%d max_round=%s %s", time.Since(started).Round(time.Millisecond), rounds, commands, total.Load(), position.Load(), volume.Load(), maxRound, soakSnapshot())
			nextLog = time.Now().Add(10 * time.Second)
		}
	}
	cancel()
	<-drained
	select {
	case err := <-eventErr:
		t.Fatalf("event reader failed: %v", err)
	default:
	}
	if position.Load() < 2 || volume.Load() < 2 {
		t.Fatalf("insufficient property notifications: position=%d volume=%d", position.Load(), volume.Load())
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	if child.ProcessState == nil || p.Running() || p.Client() != nil {
		t.Fatal("child not reaped or state retained")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temporary directory retained: %v", err)
	}
	t.Logf("PASS elapsed=%s rounds=%d commands=%d events=%d position_events=%d volume_events=%d max_round=%s final %s", time.Since(started).Round(time.Millisecond), rounds, commands, total.Load(), position.Load(), volume.Load(), maxRound, soakSnapshot())
}
