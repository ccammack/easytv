//go:build linux

package cec

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"unsafe"
)

func TestAddressConfigurationLifecycle(t *testing.T) {
	for _, scenario := range []string{
		"restart", "ownership denied", "initial clear fails",
		"claim fails", "shutdown clear fails",
	} {
		t.Run(scenario, func(t *testing.T) {
			device := filepath.Join(t.TempDir(), "fake-cec")
			if err := os.WriteFile(device, nil, 0600); err != nil {
				t.Fatal(err)
			}
			b := NewLinuxCECBackend(device)
			configured := true // Simulate retained adapter configuration.
			exclusive := false
			claimed := false
			var calls []string
			var fd int
			b.ioctlFn = func(handle int, cmd uintptr, arg unsafe.Pointer) error {
				fd = handle
				switch cmd {
				case cecAdapGCaps():
					calls = append(calls, "caps")
					caps := (*cecCaps)(arg)
					caps.Capabilities = cecCapTransmit | cecCapLogAddrs
					caps.AvailableLogAddrs = 1
				case cecAdapGPhysAddr():
					calls = append(calls, "physical")
					*(*uint16)(arg) = 0x1000
				case cecSMode():
					calls = append(calls, "exclusive")
					if scenario == "ownership denied" {
						return syscall.EBUSY
					}
					exclusive = true
				case cecAdapSLogAddrs():
					if !exclusive {
						t.Fatal("configuration changed before exclusive ownership")
					}
					addresses := (*cecLogAddrs)(arg)
					if addresses.NumLogAddrs == 0 {
						calls = append(calls, "clear")
						var stat syscall.Stat_t
						if err := syscall.Fstat(handle, &stat); err != nil {
							t.Fatalf("clear used closed descriptor: %v", err)
						}
						if scenario == "initial clear fails" ||
							(scenario == "shutdown clear fails" && claimed) {
							return syscall.EIO
						}
						configured = false
						return nil
					}
					calls = append(calls, "claim")
					if configured {
						return syscall.EBUSY
					}
					configured = true
					if scenario == "claim fails" {
						return syscall.EIO
					}
					claimed = true
					addresses.NumLogAddrs = 1
					addresses.LogAddr[0] = 4
					addresses.LogAddrMask = 1 << 4
				default:
					t.Fatalf("unexpected ioctl: %#x", cmd)
				}
				return nil
			}
			defer b.Close()

			assertClosed := func() {
				t.Helper()
				if b.file != nil || b.fd != -1 ||
					b.haveLog || b.ownsLogConfig ||
					b.logAddr != cecLogAddrInvalid {
					t.Fatal("backend retained handle or logical-address state")
				}
				var stat syscall.Stat_t
				if err := syscall.Fstat(fd, &stat); !errors.Is(err, syscall.EBADF) {
					t.Fatalf("descriptor not closed: %v", err)
				}
				if _, err := b.SelfID(); err == nil {
					t.Fatal("SelfID succeeded after close")
				}
				before := len(calls)
				if err := b.Close(); err != nil {
					t.Fatalf("second Close: %v", err)
				}
				if len(calls) != before {
					t.Fatal("second Close performed another ioctl")
				}
			}
			assertCalls := func(want ...string) {
				t.Helper()
				if !reflect.DeepEqual(calls, want) {
					t.Fatalf("calls=%v; want %v", calls, want)
				}
			}

			err := b.Open(device)
			switch scenario {
			case "ownership denied":
				if !errors.Is(err, syscall.EBUSY) {
					t.Fatalf("Open=%v; want EBUSY", err)
				}
				assertCalls("caps", "physical", "exclusive")
				if !configured {
					t.Fatal("denied owner changed retained configuration")
				}
				assertClosed()
				return
			case "initial clear fails":
				if !errors.Is(err, syscall.EIO) {
					t.Fatalf("Open=%v; want EIO", err)
				}
				assertCalls("caps", "physical", "exclusive", "clear")
				assertClosed()
				return
			case "claim fails":
				if !errors.Is(err, syscall.EIO) {
					t.Fatalf("Open=%v; want EIO", err)
				}
				assertCalls("caps", "physical", "exclusive", "clear", "claim", "clear")
				if configured {
					t.Fatal("failed claim left configuration behind")
				}
				assertClosed()
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if id, err := b.SelfID(); err != nil || id != 4 {
				t.Fatalf("SelfID=%d err=%v", id, err)
			}
			closeErr := b.Close()
			if scenario == "shutdown clear fails" {
				if !errors.Is(closeErr, syscall.EIO) {
					t.Fatalf("Close=%v; want EIO", closeErr)
				}
			} else if closeErr != nil {
				t.Fatal(closeErr)
			}
			assertCalls("caps", "physical", "exclusive", "clear", "claim", "clear")
			assertClosed()

			if scenario == "restart" {
				if configured {
					t.Fatal("shutdown retained configuration")
				}
				calls = nil
				exclusive = false
				claimed = false
				if err := b.Open(device); err != nil {
					t.Fatalf("restart: %v", err)
				}
				if err := b.Close(); err != nil {
					t.Fatal(err)
				}
				assertCalls("caps", "physical", "exclusive", "clear", "claim", "clear")
				assertClosed()
			}
		})
	}
}
