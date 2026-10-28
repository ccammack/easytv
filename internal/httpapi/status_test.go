package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type statusControls struct {
	fakeControls
	status PlaybackStatus
	err    error
}

func (c *statusControls) Status(context.Context) (PlaybackStatus, error) {
	return c.status, c.err
}

func TestStatusResponse(t *testing.T) {
	controls := &statusControls{status: PlaybackStatus{
		ChannelID: "/Channel One", FilePath: "/media/file.mp4", Playback: "stopped",
		PositionSeconds: 39, LastError: "media unavailable",
	}}
	server := NewServer(nil)
	server.SetPlaybackControls(controls)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/status", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response=%+v", response)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"channel_id", "file_path", "playback", "position_seconds", "pending_channel_id", "last_error", "next_retry_at"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("missing field %s", field)
		}
	}
	if len(fields) != 7 || string(fields["next_retry_at"]) != "null" {
		t.Fatalf("unexpected status shape: %s", response.Body.String())
	}
	var status PlaybackStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || status != controls.status {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	controls.err = errors.New("owner stopped")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/status", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", response.Code)
	}
}
