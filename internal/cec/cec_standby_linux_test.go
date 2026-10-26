//go:build linux

package cec

import "testing"

func TestStandbyRemainsAReceivedCommand(t *testing.T) {
	for _, header := range []byte{0x0f, 0x08, 0x4f, 0x40} {
		got := decodeReceivedMessage([]byte{header, 0x36}, 2)
		if got.MessageName != "STANDBY" || got.ValidationError != "" || len(got.Operands) != 0 {
			t.Fatalf("message=%+v", got)
		}
		if got.Initiator == nil || *got.Initiator != header>>4 || got.Destination == nil || *got.Destination != header&15 {
			t.Fatal("header lost")
		}
	}
}
