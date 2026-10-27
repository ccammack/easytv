package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ccammack/easytv/internal/cec"
)

const (
	receiveQueueSize   = 32
	streamWriteTimeout = 5 * time.Second
)

type receiveFormat uint8

const (
	receiveSSE receiveFormat = iota
	receiveNDJSON
)

type receivedFrame struct {
	id   string
	data json.RawMessage
}

type subscription struct {
	frames   chan receivedFrame
	overflow chan struct{}
}

func (s *Server) subscribe(capacity int) *subscription {
	sub := &subscription{frames: make(chan receivedFrame, capacity), overflow: make(chan struct{})}
	s.mu.Lock()
	s.clients[sub] = struct{}{}
	s.mu.Unlock()
	return sub
}

func (s *Server) unsubscribe(sub *subscription) {
	s.mu.Lock()
	delete(s.clients, sub)
	s.mu.Unlock()
	// Do not close frames: removal and publication share the mutex, and no
	// receiver needs channel closure. overflow is only closed on actual overflow.
}

// publishReceived never waits for subscriber I/O and does not write packet logs.
// A full queue terminates that subscription instead of skipping a packet and
// continuing an apparently intact stream. It cannot recover kernel-level loss.
func (s *Server) PublishReceived(message cec.ReceivedMessage) {
	s.mu.Lock()
	s.sequence++
	id := fmt.Sprintf("%s:%d", s.streamID, s.sequence)
	event := CECMessageReceivedEvent{ID: id, Event: "cec_message_received", DeviceName: s.deviceName, ReceivedMessage: message}
	data, err := json.Marshal(event)
	if err != nil {
		s.mu.Unlock()
		fmt.Fprintf(os.Stderr, "encode received message: %v\n", err)
		return
	}
	frame := receivedFrame{id: id, data: data}
	for sub := range s.clients {
		select {
		case sub.frames <- frame:
		default:
			delete(s.clients, sub)
			close(sub.overflow)
		}
	}
	s.mu.Unlock()
}

func (s *Server) eventsHandler(w http.ResponseWriter, r *http.Request) {
	s.streamReceived(w, r, receiveSSE)
}

func (s *Server) ndjsonHandler(w http.ResponseWriter, r *http.Request) {
	s.streamReceived(w, r, receiveNDJSON)
}

// Both transports use the same subscriptions, frames, deadlines, and overflow
// policy. Only the on-wire framing differs; packet interpretation does not.
func (s *Server) streamReceived(w http.ResponseWriter, r *http.Request, format receiveFormat) {
	// This is a live stream, not a replay endpoint. Never pretend to honor a
	// resume token while silently skipping everything received in the meantime.
	if r.Header.Get("Last-Event-ID") != "" {
		http.Error(w, "receive stream has no replay; reconnect without Last-Event-ID to start a new live subscription", http.StatusConflict)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(streamWriteTimeout)); err != nil {
		http.Error(w, "bounded streaming writes unsupported", http.StatusInternalServerError)
		return
	}
	sub := s.subscribe(receiveQueueSize)
	defer s.unsubscribe(sub)
	if format == receiveNDJSON {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-CEC-Stream-ID", s.streamID)
	flushReceivedResponse(controller)
	for {
		frame, ok := nextReceivedFrame(r.Context(), sub)
		if !ok {
			return
		}
		writeReceivedFrame(w, controller, frame, format)
	}
}

// Cancellation is a normal close. Overflow aborts the response. Check overflow
// both before waiting and after dequeueing so buffered packets are not drained
// after a known gap. Keep the existing cancellation/overflow precedence.
func nextReceivedFrame(ctx context.Context, sub *subscription) (receivedFrame, bool) {
	if ctx.Err() != nil {
		return receivedFrame{}, false
	}
	select {
	case <-sub.overflow:
		panic(http.ErrAbortHandler)
	default:
	}
	var frame receivedFrame
	select {
	case <-ctx.Done():
		return receivedFrame{}, false
	case <-sub.overflow:
		panic(http.ErrAbortHandler)
	case frame = <-sub.frames:
	}
	if ctx.Err() != nil {
		return receivedFrame{}, false
	}
	select {
	case <-sub.overflow:
		panic(http.ErrAbortHandler)
	default:
	}
	return frame, true
}

func writeReceivedFrame(w http.ResponseWriter, controller *http.ResponseController, frame receivedFrame, format receiveFormat) {
	if err := controller.SetWriteDeadline(time.Now().Add(streamWriteTimeout)); err != nil {
		panic(http.ErrAbortHandler)
	}
	var err error
	if format == receiveNDJSON {
		_, err = fmt.Fprintf(w, "%s\n", frame.data)
	} else {
		_, err = fmt.Fprintf(w, "id: %s\ndata: %s\n\n", frame.id, frame.data)
	}
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	flushReceivedResponse(controller)
}

// Flush under the active write deadline, then clear it for an idle stream.
func flushReceivedResponse(controller *http.ResponseController) {
	if err := controller.Flush(); err != nil {
		panic(http.ErrAbortHandler)
	}
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		panic(http.ErrAbortHandler)
	}
}
