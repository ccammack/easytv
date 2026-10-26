//go:build linux

package cec

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestLogicalAddressFlagValues(t *testing.T) {
	if cecLogAddrsFlAllowUnregFallback != 1 || cecLogAddrsFlAllowRcPassthru != 2 {
		t.Fatal("incorrect logical-address flags")
	}
}
func TestLinuxBackendRequiresExclusiveOwnership(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "acquired"
		if denied {
			name = "denied"
		}
		t.Run(name, func(t *testing.T) {
			device := filepath.Join(t.TempDir(), "fake-cec")
			if err := os.WriteFile(device, nil, 0600); err != nil {
				t.Fatal(err)
			}
			backend := NewLinuxCECBackend(device)
			var calls []uintptr
			var fd int
			backend.ioctlFn = func(handle int, cmd uintptr, arg unsafe.Pointer) error {
				fd = handle
				calls = append(calls, cmd)
				switch cmd {
				case cecAdapGCaps():
					caps := (*cecCaps)(arg)
					caps.Capabilities = cecCapTransmit | cecCapLogAddrs
					caps.AvailableLogAddrs = 1
				case cecAdapGPhysAddr():
					*(*uint16)(arg) = 0x1000
				case cecSMode():
					if mode := *(*uint32)(arg); mode != 0x22 {
						t.Errorf("mode=0x%x; want 0x22", mode)
					}
					if denied {
						return syscall.EBUSY
					}
				case cecAdapSLogAddrs():
					la := (*cecLogAddrs)(arg)
					if la.NumLogAddrs == 0 {
						return nil
					}
					if la.VendorID != 0xffffffff {
						t.Errorf("vendor=0x%x; want NONE", la.VendorID)
					}
					if la.Flags != 2 {
						t.Errorf("flags=0x%x; want RC passthrough only", la.Flags)
					}
					la.NumLogAddrs = 1
					la.LogAddr[0] = 4
					la.LogAddrMask = 1 << 4
				default:
					t.Errorf("unexpected ioctl 0x%x", cmd)
				}
				return nil
			}
			err := backend.Open(device)
			defer backend.Close()
			if denied {
				if !errors.Is(err, syscall.EBUSY) {
					t.Fatalf("Open=%v; want EBUSY", err)
				}
				if len(calls) != 3 {
					t.Fatalf("calls=%d; want 3, no fallback", len(calls))
				}
				if backend.file != nil || backend.fd != -1 {
					t.Fatal("failed startup retained handle")
				}
				var stat syscall.Stat_t
				if err := syscall.Fstat(fd, &stat); !errors.Is(err, syscall.EBADF) {
					t.Fatalf("descriptor not closed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			want := []uintptr{cecAdapGCaps(), cecAdapGPhysAddr(), cecSMode(), cecAdapSLogAddrs(), cecAdapSLogAddrs()}
			if len(calls) != len(want) {
				t.Fatalf("ioctl count=%d; want %d", len(calls), len(want))
			}
			for i := range want {
				if calls[i] != want[i] {
					t.Fatalf("unexpected ioctl at %d", i)
				}
			}
			if id, err := backend.SelfID(); err != nil || id != 4 {
				t.Fatalf("SelfID=%d err=%v", id, err)
			}
			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
			if backend.file != nil || backend.fd != -1 {
				t.Fatal("Close retained handle")
			}
		})
	}
}
