//go:build linux

package cec

import (
	"testing"
	"unsafe"
)

func TestCECLogAddrsLayout(t *testing.T) {
	var value cecLogAddrs

	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"LogAddrMask width", unsafe.Sizeof(value.LogAddrMask), 2},
		{"LogAddrMask offset", unsafe.Offsetof(value.LogAddrMask), 4},
		{"CecVersion offset", unsafe.Offsetof(value.CecVersion), 6},
		{"NumLogAddrs offset", unsafe.Offsetof(value.NumLogAddrs), 7},
		{"VendorID offset", unsafe.Offsetof(value.VendorID), 8},
		{"Flags offset", unsafe.Offsetof(value.Flags), 12},
		{"OsdName offset", unsafe.Offsetof(value.OsdName), 16},
		{"Features offset", unsafe.Offsetof(value.Features), 43},
		{"structure size", unsafe.Sizeof(value), 92},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if check.got != check.want {
				t.Errorf("got %d; want %d", check.got, check.want)
			}
		})
	}
}
