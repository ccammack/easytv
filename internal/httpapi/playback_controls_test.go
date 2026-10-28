package httpapi

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/catalog"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeControls struct {
	direction int
	volume    float64
	muted     bool
	calls     int
	rescanErr error
}

func (c *fakeControls) AdjustVolume(_ context.Context, delta float64) error {
	c.volume += delta
	c.calls++
	return nil
}
func (c *fakeControls) ToggleMute(context.Context) error {
	c.muted = !c.muted
	c.calls++
	return nil
}
func (c *fakeControls) ChangeChannel(_ context.Context, direction int) error {
	c.direction = direction
	c.calls++
	return nil
}
func (c *fakeControls) SetVolume(_ context.Context, value float64) error {
	c.volume = value
	c.calls++
	return nil
}
func (c *fakeControls) SetMute(_ context.Context, value bool) error {
	c.muted = value
	c.calls++
	return nil
}
func (c *fakeControls) Rescan(context.Context) error {
	c.calls++
	return c.rescanErr
}
func TestPlaybackControlValidationAndDispatch(t *testing.T) {
	controls := &fakeControls{}
	server := NewServer(nil)
	server.SetPlaybackControls(controls)
	for _, test := range []struct {
		path, body string
		want       int
	}{
		{"/channel", `{"direction":"next"}`, 204}, {"/channel", `{"direction":"previous"}`, 204},
		{"/channel", `{"direction":"up"}`, 400}, {"/channel", `{}`, 400},
		{"/volume", `{"volume":25.5}`, 204}, {"/volume", `{"volume":0}`, 204},
		{"/volume", `{"volume":101}`, 400}, {"/volume", `{"volume":-1}`, 400}, {"/volume", `{}`, 400},
		{"/volume", `{"volume":null}`, 400}, {"/volume", `{"volume":25} {}`, 400},
		{"/mute", `{"muted":true}`, 204}, {"/mute", `{"muted":false}`, 204},
		{"/mute", `{"muted":null}`, 400}, {"/mute", `{"muted":"yes"}`, 400}, {"/mute", `{"muted":true,"extra":1}`, 400},
	} {
		before := controls.calls
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest("POST", test.path, strings.NewReader(test.body)))
		if response.Code != test.want {
			t.Fatalf("%s %s status=%d", test.path, test.body, response.Code)
		}

		if test.want == 400 && controls.calls != before {
			t.Fatal("invalid request dispatched")
		}
	}
	if controls.direction != -1 || controls.volume != 0 || controls.muted || controls.calls != 6 {
		t.Fatalf("controls=%+v", controls)
	}
}

func TestRelativeAudioControls(t *testing.T) {
	controls := &fakeControls{}
	server := NewServer(nil)
	server.SetPlaybackControls(controls)
	for _, test := range []struct {
		path, body string
		want       int
	}{
		{"/volume/adjust", `{"delta":5}`, 204},
		{"/volume/adjust", `{"delta":-5}`, 204},
		{"/volume/adjust", `{}`, 400},
		{"/volume/adjust", `{"delta":null}`, 400},
		{"/volume/adjust", `{"delta":101}`, 400},
		{"/volume/adjust", `{"delta":-101}`, 400},
		{"/volume/adjust", `{"delta":5,"extra":1}`, 400},
		{"/volume/adjust", `{"delta":5} {}`, 400},
		{"/mute/toggle", `{}`, 204},
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest("POST", test.path, strings.NewReader(test.body)))
		if response.Code != test.want {
			t.Fatalf("%s %s status=%d", test.path, test.body, response.Code)
		}
	}
	if controls.calls != 3 || controls.volume != 0 || !controls.muted {
		t.Fatalf("controls=%+v", controls)
	}
}

func TestRescanHTTPContract(t *testing.T) {
	for _, test := range []struct {
		name     string
		controls *fakeControls
		want     int
	}{
		{"unavailable", nil, 503},
		{"success", &fakeControls{}, 204},
		{"busy", &fakeControls{rescanErr: catalog.ErrRescanInProgress}, 409},
		{"failure", &fakeControls{rescanErr: errors.New("scan failed")}, 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer(nil)
			if test.controls != nil {
				server.SetPlaybackControls(test.controls)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/rescan", nil))
			if response.Code != test.want || test.want == 204 && response.Body.Len() != 0 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if test.controls != nil && test.controls.calls != 1 {
				t.Fatal("rescan not dispatched")
			}
			response = httptest.NewRecorder()
			server.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/rescan", nil))
			if response.Code != 405 {
				t.Fatalf("GET status=%d", response.Code)
			}
		})
	}
}
