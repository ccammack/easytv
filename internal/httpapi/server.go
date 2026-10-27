package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/cec"
	"net"
	"net/http"
	"sync"
	"time"
)

// Backend is the context-aware serialized CEC service, never the raw adapter.
type Backend interface {
	AdapterInfo(context.Context) (cec.AdapterInfo, error)
	SetPower(context.Context, bool) error
	ClaimActiveSource(context.Context) error
}

type Server struct {
	controls   PlaybackControls
	mu         sync.Mutex
	deviceName string
	clients    map[*subscription]struct{}
	streamID   string
	sequence   uint64
	backend    Backend
}

func NewServer(deviceName string, backend Backend) *Server {
	return &Server{deviceName: deviceName, clients: make(map[*subscription]struct{}), backend: backend, streamID: rand.Text()}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /channel", s.channelHandler)
	mux.HandleFunc("POST /volume", s.volumeHandler)
	mux.HandleFunc("POST /mute", s.muteHandler)
	mux.HandleFunc("POST /rescan", s.rescanHandler)
	mux.HandleFunc("GET /adapter", s.adapterHandler)
	mux.HandleFunc("GET /events", s.eventsHandler)
	mux.HandleFunc("GET /events.ndjson", s.ndjsonHandler)
	mux.HandleFunc("POST /power", s.powerHandler)
	mux.HandleFunc("POST /active-source", s.activeSourceHandler)
	return mux
}
func (s *Server) powerHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if body.State != "on" && body.State != "off" {
		http.Error(w, "state must be on or off", http.StatusBadRequest)
		return
	}
	if s.backend == nil {
		http.Error(w, "CEC unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.backend.SetPower(r.Context(), body.State == "on"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) activeSourceHandler(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		http.Error(w, "CEC unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.backend.ClaimActiveSource(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Serve owns only HTTP, not CEC polling. The runtime publishes received packets.
// Cancel request contexts before Shutdown so idle streams can finish promptly.
func (s *Server) Serve(parent context.Context, listener net.Listener) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	server := &http.Server{Handler: s.Handler(), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	var serveErr error
	completed := false
	select {
	case <-ctx.Done():
	case serveErr = <-done:
		completed = true
	}
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := server.Shutdown(shutdownCtx)
	stop()
	if shutdownErr != nil {
		server.Close()
	}
	if !completed {
		serveErr = <-done
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	if serveErr != nil {
		serveErr = fmt.Errorf("HTTP serve: %w", serveErr)
	}
	if shutdownErr != nil {
		shutdownErr = fmt.Errorf("HTTP shutdown: %w", shutdownErr)
	}
	return errors.Join(serveErr, shutdownErr)
}
