package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type PlaybackStatus struct {
	ChannelID        string     `json:"channel_id"`
	FilePath         string     `json:"file_path"`
	Playback         string     `json:"playback"`
	PositionSeconds  int64      `json:"position_seconds"`
	PendingChannelID string     `json:"pending_channel_id"`
	LastError        string     `json:"last_error"`
	NextRetryAt      *time.Time `json:"next_retry_at"`
}

type StatusReader interface {
	Status(context.Context) (PlaybackStatus, error)
}

func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	reader, ok := s.controls.(StatusReader)
	if !ok {
		http.Error(w, "playback status unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := reader.Status(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		// Headers may already be sent; report the failed write without a second response.
		slog.Error("write playback status", "error", err)
	}
}
