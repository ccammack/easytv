//go:build linux

package cec

import "testing"

func TestDecoderPreservesAllPowerStatuses(t *testing.T) {
	for status, name := range map[byte]string{0: "on", 1: "standby", 2: "transitioning_to_on", 3: "transitioning_to_standby"} {
		got := decodeReceivedMessage([]byte{0x08, 0x90, status}, 3)
		if got.Operands["power_status_code"] != status || got.Operands["power_status"] != name {
			t.Fatalf("message=%+v", got)
		}
	}
	got := decodeReceivedMessage([]byte{0x58, 0x90, 0xff}, 3)
	if got.Raw != "5890ff" || got.Operands["power_status_code"] != byte(255) {
		t.Fatalf("unknown status lost: %+v", got)
	}
	if _, ok := got.Operands["power_status"]; ok {
		t.Fatal("invented a name for unknown power status")
	}
}
