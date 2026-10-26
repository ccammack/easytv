//go:build linux

package cec

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

func TestAdapterInfoReadsKernelMetadata(t *testing.T) {
	b := NewLinuxCECBackend("/dev/test")
	invalid := false
	b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
		switch cmd {
		case cecAdapGPhysAddr():
			if invalid {
				*(*uint16)(arg) = 0xffff
			} else {
				*(*uint16)(arg) = 0x1000
			}
		case cecAdapGLogAddrs():
			if !invalid {
				a := (*cecLogAddrs)(arg)
				a.NumLogAddrs = 1
				a.LogAddr[0] = 8
				a.LogAddrMask = 1 << 8
			}
		default:
			t.Fatalf("unexpected ioctl: %x", cmd)
		}
		return nil
	}
	got, err := b.AdapterInfo()
	if err != nil || got.PhysicalAddress == nil || *got.PhysicalAddress != "1.0.0.0" || len(got.LogicalAddresses) != 1 || got.LogicalAddresses[0] != 8 {
		t.Fatalf("info=%+v err=%v", got, err)
	}
	invalid = true
	got, err = b.AdapterInfo()
	if err != nil || got.PhysicalAddress != nil || len(got.LogicalAddresses) != 0 {
		t.Fatalf("invalid info=%+v err=%v", got, err)
	}
}

func TestAdapterInfoPropagatesReadFailures(t *testing.T) {
	for _, failure := range []uintptr{cecAdapGPhysAddr(), cecAdapGLogAddrs()} {
		b := NewLinuxCECBackend("/dev/unused")
		b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
			if cmd != cecAdapGPhysAddr() && cmd != cecAdapGLogAddrs() {
				t.Fatalf("unexpected ioctl: %x", cmd)
			}
			if cmd == failure {
				return syscall.EIO
			}
			return nil
		}
		if _, err := b.AdapterInfo(); !errors.Is(err, syscall.EIO) {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestPollPreservesPacketBytesIncludingMalformed(t *testing.T) {
	for _, length := range []uint32{0, 1, 2, 4, 17, 0xffffffff} {
		b := NewLinuxCECBackend("/dev/unused")
		b.eventReadyFn = func() (bool, error) { return false, nil }
		b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
			if cmd != cecReceive() {
				t.Fatalf("unexpected ioctl: %x", cmd)
			}
			m := (*cecMsg)(arg)
			m.Len = length
			m.Msg[0] = 0x4f
			m.Msg[1] = 0x82
			m.Msg[2] = 0x20
			return nil
		}
		packet, diagnostic, err := b.Poll()
		if err != nil || diagnostic != nil || packet == nil || packet.Length != length {
			t.Fatalf("packet=%+v diagnostic=%+v err=%v", packet, diagnostic, err)
		}
		if length == 4 {
			if packet.Raw != "4f822000" || packet.Operands["physical_address"] != "2.0.0.0" {
				t.Fatalf("packet=%+v", packet)
			}
		} else if length != 1 && packet.ValidationError == "" {
			t.Fatalf("missing length error: %+v", packet)
		}
	}
}
