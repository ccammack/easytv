package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	powerCalls, claims int
	on                 bool
	err                error
}

func (b *fakeBackend) SetPower(_ context.Context, on bool) error {
	b.powerCalls++
	b.on = on
	return b.err
}
func (b *fakeBackend) ClaimActiveSource(context.Context) error { b.claims++; return b.err }

func TestHTTPControlContract(t *testing.T) {
	backend := &fakeBackend{}
	handler := NewServer(backend).Handler()
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/adapter", "", 404},
		{"GET", "/events", "", 404},
		{"GET", "/events.ndjson", "", 404},
		{"POST", "/power", `{"state":"on"}`, 204},
		{"POST", "/power", `{"state":"off"}`, 204},
		{"POST", "/power", `{"state":"standby"}`, 400},
		{"POST", "/power", `bad json`, 400},
		{"POST", "/active-source", "", 204},
		{"GET", "/active-source", "", 405},
		{"GET", "/state", "", 404},
		{"GET", "/status", "", 404},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s %s status=%d want=%d", test.method, test.path, response.Code, test.status)
		}
	}
	if backend.powerCalls != 2 || backend.on || backend.claims != 1 {
		t.Fatal("incorrect backend command dispatch")
	}
}

func TestCECControlsReportUnavailableAndFailures(t *testing.T) {
	for _, backend := range []Backend{nil, &fakeBackend{err: errors.New("adapter failed")}} {
		for _, path := range []string{"/power", "/active-source"} {
			response := httptest.NewRecorder()
			NewServer(backend).Handler().ServeHTTP(response,
				httptest.NewRequest("POST", path, strings.NewReader(`{"state":"off"}`)))
			want := http.StatusInternalServerError
			if backend == nil {
				want = http.StatusServiceUnavailable
			}
			if response.Code != want {
				t.Fatalf("%s status=%d want=%d", path, response.Code, want)
			}
			if backend != nil && !strings.Contains(response.Body.String(), "adapter failed") {
				t.Fatal("adapter failure was not reported")
			}
		}
	}
}

func TestServerShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewServer(nil).Serve(ctx, listener) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("HTTP did not shut down")
		}
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Post("http://"+listener.Addr().String()+"/active-source", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
