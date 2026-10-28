package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"

	"github.com/ccammack/easytv/internal/catalog"
)

type PlaybackControls interface {
	ChangeChannel(context.Context, int) error
	SetVolume(context.Context, float64) error
	SetMute(context.Context, bool) error
	AdjustVolume(context.Context, float64) error
	ToggleMute(context.Context) error
	Rescan(context.Context) error
}

func (s *Server) adjustVolumeHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Delta *float64 `json:"delta"`
	}
	if err := decodeControl(w, r, &body); err != nil || body.Delta == nil ||
		math.IsNaN(*body.Delta) || math.IsInf(*body.Delta, 0) || *body.Delta < -100 || *body.Delta > 100 {
		http.Error(w, "delta must be a number from -100 to 100", http.StatusBadRequest)
		return
	}
	if s.controls == nil {
		http.Error(w, "playback unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.controls.AdjustVolume(r.Context(), *body.Delta); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) toggleMuteHandler(w http.ResponseWriter, r *http.Request) {
	if s.controls == nil {
		http.Error(w, "playback unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.controls.ToggleMute(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rescanHandler(w http.ResponseWriter, r *http.Request) {
	if s.controls == nil {
		http.Error(w, "playback unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.controls.Rescan(r.Context()); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, catalog.ErrRescanInProgress) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Configure only before serving requests.
func (s *Server) SetPlaybackControls(controls PlaybackControls) { s.controls = controls }
func decodeControl(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}
func (s *Server) channelHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Direction string `json:"direction"`
	}
	if err := decodeControl(w, r, &body); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	direction := 0
	switch body.Direction {
	case "next":
		direction = 1
	case "previous":
		direction = -1
	default:
		http.Error(w, "direction must be next or previous", 400)
		return
	}
	if s.controls == nil {
		http.Error(w, "playback unavailable", 503)
		return
	}
	if err := s.controls.ChangeChannel(r.Context(), direction); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Acceptance includes AS success and preview scheduling, not delayed commit.
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) volumeHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Volume *float64 `json:"volume"`
	}
	if err := decodeControl(w, r, &body); err != nil || body.Volume == nil {
		http.Error(w, "volume required", 400)
		return
	}
	value := *body.Volume
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		http.Error(w, "volume must be from 0 to 100", 400)
		return
	}
	if s.controls == nil {
		http.Error(w, "playback unavailable", 503)
		return
	}
	if err := s.controls.SetVolume(r.Context(), value); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) muteHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Muted *bool `json:"muted"`
	}
	if err := decodeControl(w, r, &body); err != nil || body.Muted == nil {
		http.Error(w, "muted boolean required", 400)
		return
	}
	if s.controls == nil {
		http.Error(w, "playback unavailable", 503)
		return
	}
	if err := s.controls.SetMute(r.Context(), *body.Muted); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
