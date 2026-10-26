package cec

import (
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestReceivedMessageDecoding(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		message string
		key     string
		value   any
	}{
		{"route", []byte{0x0f, 0x86, 0x10, 0}, "SET_STREAM_PATH", "physical_address", "1.0.0.0"},
		{"active", []byte{0x4f, 0x82, 0x20, 0}, "ACTIVE_SOURCE", "physical_address", "2.0.0.0"},
		{"change", []byte{0x0f, 0x80, 0x10, 0, 0x20, 0}, "ROUTING_CHANGE", "new_physical_address", "2.0.0.0"},
		{"power", []byte{0x08, 0x90, 1}, "REPORT_POWER_STATUS", "power_status", "standby"},
		{"standby", []byte{0x0f, 0x36}, "STANDBY", "", nil},
		{"identity", []byte{0x4f, 0x84, 0x20, 0, 4}, "REPORT_PHYSICAL_ADDRESS", "physical_address", "2.0.0.0"},
		{"abort", []byte{0x40, 0, 0x86, 4}, "FEATURE_ABORT", "aborted_opcode", byte(0x86)},
		{"unknown", []byte{0x4f, 0xfe, 0x12}, "UNKNOWN", "", nil},
		{"poll", []byte{0x04}, "POLL", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeReceivedMessage(tt.data, uint32(len(tt.data)))
			if got.ValidationError != "" || got.MessageName != tt.message || got.ReceivedAt.IsZero() {
				t.Fatalf("message=%+v", got)
			}
			if got.Initiator == nil || got.Destination == nil || *got.Initiator != tt.data[0]>>4 || *got.Destination != tt.data[0]&15 {
				t.Fatal("incorrect header")
			}
			if tt.key != "" && got.Operands[tt.key] != tt.value {
				t.Fatalf("operands=%v", got.Operands)
			}
		})
	}
}

func TestReceivedMalformedMessages(t *testing.T) {
	for _, data := range [][]byte{nil, {0x0f, 0x82}, {0x0f, 0x86, 0x10}, {0x08, 0x90}, {0x0f, 0x80, 0, 0}} {
		if decodeReceivedMessage(data, uint32(len(data))).ValidationError == "" {
			t.Fatalf("accepted malformed packet: %x", data)
		}
	}
	if decodeReceivedMessage(make([]byte, 16), 17).ValidationError == "" {
		t.Fatal("accepted oversize message")
	}
}

func TestDecoderPreservesRawAndOptionalFields(t *testing.T) {
	for _, data := range [][]byte{{0x4f, 0x82, 0x20, 0}, {0x4f, 0xfe, 0x12}, {0x4f, 0x82}, {0x04}, nil} {
		first := decodeReceivedMessage(data, uint32(len(data)))
		second := decodeReceivedMessage(data, uint32(len(data)))
		want := hex.EncodeToString(data)
		if first.Raw != want || second.Raw != want {
			t.Fatalf("raw bytes changed: %x", data)
		}
		if len(data) < 2 && (first.Opcode != nil || second.Opcode != nil) {
			t.Fatal("invented opcode")
		}
		if len(data) == 0 && (first.Initiator != nil || first.Destination != nil) {
			t.Fatal("invented header")
		}
	}
	route := decodeReceivedMessage([]byte{0x0f, 0x80, 0x20, 0, 0x10, 0}, 6)
	if route.Operands["old_physical_address"] != "2.0.0.0" || route.Operands["new_physical_address"] != "1.0.0.0" {
		t.Fatalf("route=%+v", route)
	}
	poll := decodeReceivedMessage([]byte{0x04}, 1)
	encoded, err := json.Marshal(poll)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["opcode"]) != "null" || string(fields["initiator"]) != "0" || string(fields["destination"]) != "4" {
		t.Fatalf("json=%s", encoded)
	}
}
