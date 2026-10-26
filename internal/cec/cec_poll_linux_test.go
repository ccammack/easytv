//go:build linux

package cec

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

func TestPollEmptyEventQueueUsesBoundedReceive(t *testing.T) {
	b := NewLinuxCECBackend("/dev/unused")
	b.eventReadyFn = func() (bool, error) { return false, nil }
	received := false
	b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
		if cmd != cecReceive() || (*cecMsg)(arg).Timeout != 500 {
			t.Fatal("expected bounded receive")
		}
		received = true
		return syscall.ETIMEDOUT
	}
	message, diagnostic, err := b.Poll()
	if message != nil || diagnostic != nil || err != nil || !received {
		t.Fatalf("message=%+v diagnostic=%+v err=%v", message, diagnostic, err)
	}
}

func TestPollPropagatesEventFailures(t *testing.T) {
	for _, readinessFailure := range []bool{false, true} {
		b := NewLinuxCECBackend("/dev/unused")
		b.eventReadyFn = func() (bool, error) {
			if readinessFailure {
				return false, syscall.EIO
			}
			return true, nil
		}
		b.ioctlFn = func(_ int, cmd uintptr, _ unsafe.Pointer) error {
			if readinessFailure || cmd != cecDQEvent() {
				t.Fatal("unexpected ioctl")
			}
			return syscall.EIO
		}
		if _, _, err := b.Poll(); !errors.Is(err, syscall.EIO) {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestPollSeparatesAdapterEventsFromPackets(t *testing.T) {
	for _, kind := range []uint32{cecEventStateChange, cecEventLostMsgs, 99} {
		b := NewLinuxCECBackend("/dev/unused")
		b.eventReadyFn = func() (bool, error) { return true, nil }
		b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
			if cmd != cecDQEvent() {
				t.Fatal("unexpected receive")
			}
			e := (*cecEvent)(arg)
			e.Event = kind
			e.Ts = 123
			e.Flags = 1
			if kind == cecEventStateChange {
				e.Raw[0] = uint32(1<<8)<<16 | 0x1000
			} else {
				e.Raw[0] = 7
			}
			return nil
		}
		packet, event, err := b.Poll()
		if err != nil || packet != nil || event == nil || event.Event != kind || event.KernelTimestampNS != 123 || event.Flags != 1 {
			t.Fatalf("packet=%+v event=%+v err=%v", packet, event, err)
		}
		if kind == cecEventStateChange && (event.PhysicalAddress == nil || *event.PhysicalAddress != "1.0.0.0" || event.LogicalAddressMask == nil || *event.LogicalAddressMask != 1<<8) {
			t.Fatalf("event=%+v", event)
		}
		if kind == cecEventLostMsgs && (event.LostMessages == nil || *event.LostMessages != 7) {
			t.Fatalf("event=%+v", event)
		}
	}
}

func TestPollEmptyDequeueRaceContinuesReceiving(t *testing.T) {
	for _, empty := range []error{syscall.EAGAIN, syscall.ENODATA} {
		b := NewLinuxCECBackend("/dev/unused")
		b.eventReadyFn = func() (bool, error) { return true, nil }
		calls := 0
		b.ioctlFn = func(_ int, cmd uintptr, _ unsafe.Pointer) error {
			calls++
			if calls == 1 && cmd == cecDQEvent() {
				return empty
			}
			if calls == 2 && cmd == cecReceive() {
				return syscall.ETIMEDOUT
			}
			t.Fatal("unexpected ioctl")
			return nil
		}
		packet, event, err := b.Poll()
		if err != nil || packet != nil || event != nil || calls != 2 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}
