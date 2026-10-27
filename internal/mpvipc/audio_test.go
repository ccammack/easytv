package mpvipc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"reflect"
	"testing"
	"time"
)

func expectAudioCommand(server net.Conn, want []any, data json.RawMessage) <-chan error {
	result := make(chan error, 1)
	go func() {
		var request struct {
			RequestID uint64 `json:"request_id"`
			Command   []any  `json:"command"`
		}
		if err := json.NewDecoder(server).Decode(&request); err != nil {
			result <- err
			return
		}
		if !reflect.DeepEqual(request.Command, want) {
			result <- fmt.Errorf("command=%v want=%v", request.Command, want)
			_ = server.Close()
			return
		}
		response := struct {
			RequestID uint64          `json:"request_id"`
			Error     string          `json:"error"`
			Data      json.RawMessage `json:"data"`
		}{request.RequestID, "success", data}
		result <- json.NewEncoder(server).Encode(response)
	}()
	return result
}
func TestSetAudioCommands(t *testing.T) {
	for _, value := range []float64{0, 25.5, 100} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			c, server := pair(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := expectAudioCommand(server, []any{"set_property", "volume", value}, json.RawMessage("null"))
			if err := c.SetVolume(ctx, value); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, muted := range []bool{false, true} {
		t.Run(fmt.Sprint(muted), func(t *testing.T) {
			c, server := pair(t)
			done := expectAudioCommand(server, []any{"set_property", "mute", muted}, json.RawMessage("null"))
			if err := c.SetMute(context.Background(), muted); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestInvalidVolumeDoesNotSendCommand(t *testing.T) {
	c, _ := pair(t)
	for _, value := range []float64{-1, 101, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := c.SetVolume(context.Background(), value); err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
	c.mu.Lock()
	requests := c.next
	c.mu.Unlock()
	if requests != 0 {
		t.Fatal("invalid volume sent a command")
	}
}
func TestReadAudioProperties(t *testing.T) {
	c, server := pair(t)
	done := expectAudioCommand(server, []any{"get_property", "volume"}, json.RawMessage("37.5"))
	if value, err := c.Volume(context.Background()); err != nil || value != 37.5 {
		t.Fatalf("volume=%v err=%v", value, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	done = expectAudioCommand(server, []any{"get_property", "mute"}, json.RawMessage("true"))
	if value, err := c.Muted(context.Background()); err != nil || !value {
		t.Fatalf("mute=%v err=%v", value, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestInvalidAudioReplies(t *testing.T) {
	for _, text := range []string{"null", `"loud"`, "-1", "101"} {
		t.Run("volume/"+text, func(t *testing.T) {
			c, server := pair(t)
			done := expectAudioCommand(server, []any{"get_property", "volume"}, json.RawMessage(text))
			if _, err := c.Volume(context.Background()); err == nil {
				t.Fatal("invalid volume reply accepted")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, text := range []string{"null", `"yes"`, "1"} {
		t.Run("mute/"+text, func(t *testing.T) {
			c, server := pair(t)
			done := expectAudioCommand(server, []any{"get_property", "mute"}, json.RawMessage(text))
			if _, err := c.Muted(context.Background()); err == nil {
				t.Fatal("invalid mute reply accepted")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestAudioOperationsPropagateClosedConnection(t *testing.T) {
	c, _ := pair(t)
	_ = c.Close()
	if err := c.SetVolume(context.Background(), 50); err == nil {
		t.Fatal("set volume ignored failure")
	}
	if err := c.SetMute(context.Background(), true); err == nil {
		t.Fatal("set mute ignored failure")
	}
	if _, err := c.Volume(context.Background()); err == nil {
		t.Fatal("get volume ignored failure")
	}
	if _, err := c.Muted(context.Background()); err == nil {
		t.Fatal("get mute ignored failure")
	}
}
