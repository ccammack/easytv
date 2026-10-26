//go:build linux

package cec

import (
	"errors"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"unsafe"
)

func TestExplicitCommandsReadCurrentAddressesEveryTime(t *testing.T) {
	b := NewLinuxCECBackend("/dev/unused")
	// Deliberately stale startup bookkeeping must not gate or address commands.
	b.logAddr = 4
	b.haveLog = false
	logical, physical := uint8(8), uint16(0x1000)
	var transmitted [][]byte
	var calls []uintptr
	b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
		calls = append(calls, cmd)
		switch cmd {
		case cecAdapGLogAddrs():
			a := (*cecLogAddrs)(arg)
			a.NumLogAddrs = 1
			a.LogAddr[0] = logical
			a.LogAddrMask = uint16(1) << logical
		case cecAdapGPhysAddr():
			*(*uint16)(arg) = physical
		case cecTransmit():
			m := (*cecMsg)(arg)
			if m.Timeout != 1000 {
				t.Fatalf("transmit timeout=%d", m.Timeout)
			}
			transmitted = append(transmitted, append([]byte(nil), m.Msg[:m.Len]...))
			m.TxStatus = cecTxStatusOK
		default:
			t.Fatalf("unexpected ioctl: %x", cmd)
		}
		return nil
	}
	if err := b.ClaimActiveSource(); err != nil {
		t.Fatal(err)
	}
	logical, physical = 11, 0x2300
	if err := b.ClaimActiveSource(); err != nil {
		t.Fatal(err)
	}
	if err := b.SetPower(true); err != nil {
		t.Fatal(err)
	}
	logical = 8
	if err := b.SetPower(false); err != nil {
		t.Fatal(err)
	}
	want := [][]byte{{0x8f, 0x82, 0x10, 0}, {0xbf, 0x82, 0x23, 0}, {0xb0, 0x04}, {0x8f, 0x36}}
	if !reflect.DeepEqual(transmitted, want) {
		t.Fatalf("transmitted=%x want=%x", transmitted, want)
	}
	wantCalls := []uintptr{cecAdapGLogAddrs(), cecAdapGPhysAddr(), cecTransmit(), cecAdapGLogAddrs(), cecAdapGPhysAddr(), cecTransmit(), cecAdapGLogAddrs(), cecTransmit(), cecAdapGLogAddrs(), cecTransmit()}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls=%v want=%v", calls, wantCalls)
	}
	if b.logAddr != 4 || b.haveLog {
		t.Fatal("commands changed shared startup bookkeeping")
	}
}

func TestCommandsRejectUnavailableLogicalAddresses(t *testing.T) {
	cases := []struct {
		name    string
		count   uint8
		address uint8
		mask    uint16
	}{
		{"none", 0, 0, 0}, {"invalid", 1, 0xff, 0}, {"unregistered", 1, 15, 1 << 15},
		{"mask mismatch", 1, 8, 1 << 4}, {"oversize count", 5, 8, 1 << 8},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for _, operation := range []string{"active", "on", "off"} {
				b := NewLinuxCECBackend("/dev/unused")
				b.haveLog = true
				b.logAddr = 4
				b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
					if cmd != cecAdapGLogAddrs() {
						t.Fatalf("unavailable logical address reached ioctl: %x", cmd)
					}
					a := (*cecLogAddrs)(arg)
					a.NumLogAddrs = tt.count
					a.LogAddr[0] = tt.address
					a.LogAddrMask = tt.mask
					return nil
				}
				var err error
				if operation == "active" {
					err = b.ClaimActiveSource()
				} else {
					err = b.SetPower(operation == "on")
				}
				if err == nil {
					t.Fatalf("%s succeeded with unavailable logical address", operation)
				}
			}
		})
	}
}

func TestTransmitLogicalAddressRequiresListAndMaskAgreement(t *testing.T) {
	b := NewLinuxCECBackend("/dev/unused")
	b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
		if cmd != cecAdapGLogAddrs() {
			t.Fatal("unexpected ioctl")
		}
		a := (*cecLogAddrs)(arg)
		a.NumLogAddrs = 2
		a.LogAddr[0] = 0xff
		a.LogAddr[1] = 11
		a.LogAddrMask = 1 << 11
		return nil
	}
	address, err := b.currentTransmitLogicalAddress()
	if err != nil || address != 11 {
		t.Fatalf("address=%d err=%v", address, err)
	}
}

func TestActiveSourceRejectsUnavailableOrMalformedPhysicalAddress(t *testing.T) {
	for _, physical := range []uint16{0xffff, 0x0100, 0x1020, 0x1001} {
		b := NewLinuxCECBackend("/dev/unused")
		b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
			switch cmd {
			case cecAdapGLogAddrs():
				a := (*cecLogAddrs)(arg)
				a.NumLogAddrs = 1
				a.LogAddr[0] = 8
				a.LogAddrMask = 1 << 8
			case cecAdapGPhysAddr():
				*(*uint16)(arg) = physical
			default:
				t.Fatalf("invalid physical address reached transmit: %x", cmd)
			}
			return nil
		}
		if err := b.ClaimActiveSource(); err == nil {
			t.Fatalf("accepted invalid physical address: %04x", physical)
		}
	}
}

func TestTransmitPhysicalAddressAcceptsValidHierarchy(t *testing.T) {
	for _, physical := range []uint16{0, 0x1000, 0x2300, 0x1230, 0x1234, 0xf000, 0xfff0} {
		b := NewLinuxCECBackend("/dev/unused")
		b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
			if cmd != cecAdapGPhysAddr() {
				t.Fatal("unexpected ioctl")
			}
			*(*uint16)(arg) = physical
			return nil
		}
		got, err := b.currentTransmitPhysicalAddress()
		if err != nil || got != physical {
			t.Fatalf("physical=%04x got=%04x err=%v", physical, got, err)
		}
	}
}

func TestCommandAddressAndTransmitErrorsPropagate(t *testing.T) {
	for _, operation := range []string{"active", "on", "off"} {
		stages := []uintptr{cecAdapGLogAddrs(), cecTransmit()}
		if operation == "active" {
			stages = append(stages, cecAdapGPhysAddr())
		}
		for _, failure := range stages {
			b := NewLinuxCECBackend("/dev/unused")
			failed := false
			b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
				if failed {
					t.Fatal("continued after query/transmit failure")
				}
				if cmd == failure {
					failed = true
					return syscall.EIO
				}
				switch cmd {
				case cecAdapGLogAddrs():
					a := (*cecLogAddrs)(arg)
					a.NumLogAddrs = 1
					a.LogAddr[0] = 8
					a.LogAddrMask = 1 << 8
				case cecAdapGPhysAddr():
					*(*uint16)(arg) = 0x1000
				default:
					t.Fatalf("unexpected ioctl: %x", cmd)
				}
				return nil
			}
			var err error
			if operation == "active" {
				err = b.ClaimActiveSource()
			} else {
				err = b.SetPower(operation == "on")
			}
			if !failed || !errors.Is(err, syscall.EIO) {
				t.Fatalf("operation=%s failed=%t err=%v", operation, failed, err)
			}
		}
	}
}

func TestCommandStillChecksTransmitStatus(t *testing.T) {
	for _, operation := range []string{"active", "on", "off"} {
		b := NewLinuxCECBackend("/dev/unused")
		b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
			switch cmd {
			case cecAdapGLogAddrs():
				a := (*cecLogAddrs)(arg)
				a.NumLogAddrs = 1
				a.LogAddr[0] = 8
				a.LogAddrMask = 1 << 8
			case cecAdapGPhysAddr():
				*(*uint16)(arg) = 0x1000
			case cecTransmit():
				(*cecMsg)(arg).TxStatus = cecTxStatusNack
			default:
				t.Fatal("unexpected ioctl")
			}
			return nil
		}
		var err error
		if operation == "active" {
			err = b.ClaimActiveSource()
		} else {
			err = b.SetPower(operation == "on")
		}
		if err == nil {
			t.Fatalf("%s ignored failed transmit status", operation)
		}
	}
}

func TestConcurrentCommandsUseIndependentAddressStorage(t *testing.T) {
	b := NewLinuxCECBackend("/dev/unused")
	b.logAddr = 4
	b.haveLog = false
	b.ioctlFn = func(_ int, cmd uintptr, arg unsafe.Pointer) error {
		switch cmd {
		case cecAdapGLogAddrs():
			a := (*cecLogAddrs)(arg)
			a.NumLogAddrs = 1
			a.LogAddr[0] = 8
			a.LogAddrMask = 1 << 8
		case cecAdapGPhysAddr():
			*(*uint16)(arg) = 0x1000
		case cecTransmit():
			m := (*cecMsg)(arg)
			if m.Msg[0]>>4 != 8 {
				t.Errorf("wrong initiator: %x", m.Msg[0])
			}
			if m.Msg[1] == cecMsgActiveSource && (m.Len != 4 || m.Msg[2] != 0x10 || m.Msg[3] != 0) {
				t.Errorf("wrong AS payload: %x", m.Msg[:m.Len])
			}
			m.TxStatus = cecTxStatusOK
		default:
			t.Error("unexpected ioctl")
		}
		return nil
	}
	var workers sync.WaitGroup
	results := make(chan error, 60)
	for i := 0; i < 20; i++ {
		workers.Add(3)
		go func() { defer workers.Done(); results <- b.ClaimActiveSource() }()
		go func() { defer workers.Done(); results <- b.SetPower(true) }()
		go func() { defer workers.Done(); results <- b.SetPower(false) }()
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if b.logAddr != 4 || b.haveLog {
		t.Fatal("commands mutated startup bookkeeping")
	}
}
