package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/ccammack/easytv/internal/cec"
	"io"
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
}

func (b *fakeBackend) AdapterInfo(context.Context) (cec.AdapterInfo, error) {
	physical := "1.0.0.0"
	return cec.AdapterInfo{DevicePath: "mock-only", LogicalAddresses: []int{8}, PhysicalAddress: &physical}, nil
}
func (b *fakeBackend) SetPower(_ context.Context, on bool) error {
	b.powerCalls++
	b.on = on
	return nil
}
func (b *fakeBackend) ClaimActiveSource(context.Context) error { b.claims++; return nil }

func TestLegacyHTTPContract(t *testing.T) {
	backend := &fakeBackend{}
	server := NewServer("easytv", backend)
	handler := server.Handler()
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/adapter", "", 200},
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
		if test.path == "/adapter" {
			var result struct {
				DeviceName string `json:"device_name"`
				cec.AdapterInfo
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.DeviceName != "easytv" || len(result.LogicalAddresses) != 1 || result.LogicalAddresses[0] != 8 {
				t.Fatal("adapter response changed")
			}
		}
	}
	if backend.powerCalls != 2 || backend.on || backend.claims != 1 {
		t.Fatal("incorrect backend command dispatch")
	}
	if server.sequence != 0 {
		t.Fatal("commands manufactured receive-stream events")
	}
}

func TestStreamsSharePublicationAndCloseOnShutdown(t *testing.T) {
	server := NewServer("easytv", &fakeBackend{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
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
	sse, err := client.Get("http://" + address + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	ndjson, err := client.Get("http://" + address + "/events.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer ndjson.Body.Close()
	if sse.Header.Get("Content-Type") != "text/event-stream" || ndjson.Header.Get("Content-Type") != "application/x-ndjson; charset=utf-8" {
		t.Fatal("stream content types changed")
	}
	if sse.Header.Get("X-CEC-Stream-ID") == "" || sse.Header.Get("X-CEC-Stream-ID") != ndjson.Header.Get("X-CEC-Stream-ID") {
		t.Fatal("stream IDs differ")
	}
	scanner := bufio.NewScanner(sse.Body)
	decoder := json.NewDecoder(ndjson.Body)
	previous := ""
	for i := 0; i < 2; i++ {
		server.PublishReceived(cec.ReceivedMessage{Raw: "4f822000", Length: 4, MessageName: "ACTIVE_SOURCE"})
		if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "id: ") {
			t.Fatal("missing SSE id")
		}
		id := strings.TrimPrefix(scanner.Text(), "id: ")
		if id == previous {
			t.Fatal("repeated packet deduplicated")
		}
		previous = id
		if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "data: ") {
			t.Fatal("missing SSE data")
		}
		var event CECMessageReceivedEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() || scanner.Text() != "" {
			t.Fatal("missing SSE frame separator")
		}
		var line CECMessageReceivedEvent
		if err := decoder.Decode(&line); err != nil {
			t.Fatal(err)
		}
		if event.ID != id || line.ID != id || event.Raw != "4f822000" || line.Raw != event.Raw || event.Event != "cec_message_received" {
			t.Fatal("wire payload mismatch")
		}
	}
	request, err := http.NewRequest("GET", "http://"+address+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", previous)
	rejected, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	rejected.Body.Close()
	if rejected.StatusCode != 409 {
		t.Fatal("replay token not rejected")
	}
	cancel()
	rest, err := io.ReadAll(sse.Body)
	if err != nil || len(rest) != 0 {
		t.Fatalf("idle SSE did not close cleanly: %v %q", err, rest)
	}
}

func TestSubscriberOverflowAbortsInsteadOfSkipping(t *testing.T) {
	server := NewServer("easytv", &fakeBackend{})
	sub := server.subscribe(1)
	server.PublishReceived(cec.ReceivedMessage{Raw: "00", Length: 1})
	server.PublishReceived(cec.ReceivedMessage{Raw: "00", Length: 1})
	defer func() {
		value := recover()
		err, ok := value.(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("overflow panic=%v", value)
		}
	}()
	nextReceivedFrame(context.Background(), sub)
}
