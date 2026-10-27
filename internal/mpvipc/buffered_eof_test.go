package mpvipc

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestBufferedEOFDeliveredAfterDisconnect(t *testing.T) {
	c, server := pair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { fmt.Fprintln(server, `{"event":"end-file","reason":"eof"}`); server.Close() }()
	select {
	case <-c.done:
	case <-ctx.Done():
		t.Fatal("client did not detect disconnect")
	}
	raw, err := c.NextEvent(ctx)
	if err != nil || string(raw) != `{"event":"end-file","reason":"eof"}` {
		t.Fatalf("lost queued EOF: event=%s err=%v", raw, err)
	}
	if _, err := c.NextEvent(ctx); err == nil {
		t.Fatal("disconnect not reported after event queue drained")
	}
}
