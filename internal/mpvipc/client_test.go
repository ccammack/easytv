package mpvipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func pair(t *testing.T) (*Client, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	c := New(a)
	t.Cleanup(func() { _ = c.Close(); _ = b.Close() })
	return c, b
}
func TestReplyAndEventAreSeparated(t *testing.T) {
	c, server := pair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		var request struct {
			RequestID uint64 `json:"request_id"`
		}
		if json.NewDecoder(server).Decode(&request) != nil {
			return
		}
		fmt.Fprintln(server, `{"event":"file-loaded"}`)
		fmt.Fprintf(server, `{"request_id":%d,"error":"success","data":42}`+"\n", request.RequestID)
	}()
	data, err := c.Call(ctx, "get_property", "time-pos")
	if err != nil || string(data) != "42" {
		t.Fatalf("data=%s err=%v", data, err)
	}
	event, err := c.NextEvent(ctx)
	if err != nil || string(event) != `{"event":"file-loaded"}` {
		t.Fatalf("event=%s err=%v", event, err)
	}
}
func TestConcurrentRepliesCanArriveOutOfOrder(t *testing.T) {
	c, server := pair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		decoder := json.NewDecoder(server)
		var requests [2]struct {
			RequestID uint64   `json:"request_id"`
			Command   []string `json:"command"`
		}
		for i := range requests {
			if decoder.Decode(&requests[i]) != nil {
				return
			}
		}
		for i := 1; i >= 0; i-- {
			fmt.Fprintf(server, `{"request_id":%d,"error":"success","data":%q}`+"\n", requests[i].RequestID, requests[i].Command[0])
		}
	}()
	results := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		go func(name string) {
			data, err := c.Call(ctx, name)
			if err == nil && string(data) != fmt.Sprintf("%q", name) {
				err = fmt.Errorf("wrong reply %s", data)
			}
			results <- err
		}(name)
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
func TestCommandError(t *testing.T) {
	c, server := pair(t)
	go func() {
		var request struct {
			RequestID uint64 `json:"request_id"`
		}
		if json.NewDecoder(server).Decode(&request) == nil {
			fmt.Fprintf(server, `{"request_id":%d,"error":"property unavailable"}`+"\n", request.RequestID)
		}
	}()
	if _, err := c.Call(context.Background(), "get_property", "missing"); err == nil {
		t.Fatal("command error ignored")
	}
}
func TestReplyTimeoutCleansPending(t *testing.T) {
	c, server := pair(t)
	go func() { _, _ = bufio.NewReader(server).ReadString('\n') }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "get_property", "time-pos"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	c.mu.Lock()
	count := len(c.pending)
	c.mu.Unlock()
	if count != 0 {
		t.Fatal("pending request retained")
	}
}
func TestDisconnectUnblocksCall(t *testing.T) {
	c, server := pair(t)
	go func() { _, _ = bufio.NewReader(server).ReadString('\n'); _ = server.Close() }()
	if _, err := c.Call(context.Background(), "get_property", "time-pos"); err == nil {
		t.Fatal("disconnect ignored")
	}
}
func TestMalformedMessageClosesClient(t *testing.T) {
	c, server := pair(t)
	go func() { _, _ = fmt.Fprintln(server, "not json") }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.NextEvent(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("malformed message did not close client")
	}
}
func TestCloseIsIdempotent(t *testing.T) {
	c, _ := pair(t)
	_ = c.Close()
	_ = c.Close()
	if _, err := c.Call(context.Background(), "stop"); !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}
