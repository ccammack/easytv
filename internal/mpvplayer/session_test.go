package mpvplayer

import (
	"context"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/mpvipc"
	"net"
	"testing"
	"time"
)

func TestSessionDrainsEOFBeforeProcessExit(t *testing.T) {
	a, b := net.Pipe()
	client := mpvipc.New(a)
	defer client.Close()
	defer b.Close()
	childDone := make(chan struct{})
	close(childDone)
	session := Session{client: client, done: childDone, generation: 42}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out := make(chan Event)
	finished := make(chan error, 1)
	go func() { finished <- session.Watch(ctx, out) }()
	go func() { fmt.Fprintln(b, `{"event":"end-file","reason":"eof"}`); b.Close() }()
	for _, want := range []string{"end-file", "process-exit"} {
		select {
		case event := <-out:
			if event.Kind != want || event.Generation != 42 {
				t.Fatalf("event=%+v want=%s", event, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestSessionCancellationUnblocksUndrainedOutput(t *testing.T) {
	a, b := net.Pipe()
	client := mpvipc.New(a)
	defer client.Close()
	defer b.Close()
	session := Session{client: client, done: make(chan struct{}), generation: 1}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- session.Watch(ctx, make(chan Event)) }()
	written := make(chan struct{})
	go func() { fmt.Fprintln(b, `{"event":"start-file"}`); close(written) }()
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("event write blocked")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher failed to join workers on cancellation")
	}
}

func TestSessionRequiresActivePlayer(t *testing.T) {
	if _, err := New(Config{}).Session(); err == nil {
		t.Fatal("inactive player provided session")
	}
	if err := (Session{}).Watch(context.Background(), make(chan Event)); err == nil {
		t.Fatal("invalid session accepted")
	}
}
