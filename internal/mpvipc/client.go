package mpvipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type result struct {
	data json.RawMessage
	err  error
}

// Client multiplexes replies and events over an already-connected mpv socket.
// It owns the connection. An event-queue overflow terminates the connection
// instead of silently dropping a potentially important end-file notification.
type Client struct {
	conn    net.Conn
	mu      sync.Mutex
	writeMu sync.Mutex
	next    uint64
	pending map[uint64]chan result
	events  chan json.RawMessage
	done    chan struct{}
	err     error
}

func New(conn net.Conn) *Client {
	c := &Client{conn: conn, pending: make(map[uint64]chan result), events: make(chan json.RawMessage, 64), done: make(chan struct{})}
	go c.readLoop()
	return c
}
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	close(c.done)
	c.mu.Unlock()
	_ = c.conn.Close()
}
func (c *Client) Close() error   { c.fail(net.ErrClosed); return nil }
func (c *Client) failure() error { c.mu.Lock(); defer c.mu.Unlock(); return c.err }

// Call has a default three-second timeout if the caller has no deadline.
func (c *Client) Call(ctx context.Context, command ...any) (json.RawMessage, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.next++
	id := c.next
	reply := make(chan result, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	payload, err := json.Marshal(struct {
		Command   []any  `json:"command"`
		RequestID uint64 `json:"request_id"`
	}{command, id})
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')
	c.writeMu.Lock()
	if err = ctx.Err(); err == nil {
		deadline, _ := ctx.Deadline()
		err = c.conn.SetWriteDeadline(deadline)
		if err == nil {
			for len(payload) > 0 {
				var n int
				n, err = c.conn.Write(payload)
				if err != nil {
					break
				}
				if n == 0 {
					err = errors.New("mpv IPC write made no progress")
					break
				}
				payload = payload[n:]
			}
		}
	}
	c.writeMu.Unlock()
	if err != nil {
		// A partial write can corrupt framing; do not reuse this connection.
		c.fail(fmt.Errorf("write mpv command: %w", err))
		return nil, err
	}
	select {
	case r := <-reply:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.failure()
	}
}

// NextEvent preserves events already received before the socket closed.
func (c *Client) NextEvent(ctx context.Context) (json.RawMessage, error) {
	select {
	case event := <-c.events:
		return event, nil
	default:
	}
	select {
	case event := <-c.events:
		return event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		select {
		case event := <-c.events:
			return event, nil
		default:
			return nil, c.failure()
		}
	}
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := append(json.RawMessage(nil), scanner.Bytes()...)
		if len(line) == 0 {
			continue
		}
		var message struct {
			RequestID *uint64         `json:"request_id"`
			Error     string          `json:"error"`
			Data      json.RawMessage `json:"data"`
			Event     string          `json:"event"`
		}
		if err := json.Unmarshal(line, &message); err != nil {
			c.fail(fmt.Errorf("decode mpv IPC: %w", err))
			return
		}
		if message.RequestID != nil {
			r := result{data: message.Data}
			if message.Error != "success" {
				r.err = fmt.Errorf("mpv command failed: %s", message.Error)
			}
			c.mu.Lock()
			ch := c.pending[*message.RequestID]
			delete(c.pending, *message.RequestID)
			c.mu.Unlock()
			if ch != nil {
				ch <- r
			}
		} else if message.Event != "" {
			select {
			case c.events <- line:
			default:
				c.fail(errors.New("mpv event queue overflow"))
				return
			}
		}
	}
	err := scanner.Err()
	if err == nil {
		err = errors.New("mpv IPC connection closed")
	}
	c.fail(err)
}
