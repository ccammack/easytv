//go:build linux

package cec

import (
	"fmt"
	"unsafe"
)

// AdapterInfo reads kernel metadata, without transmitting CEC messages.
func (b *LinuxCECBackend) AdapterInfo() (AdapterInfo, error) {
	info := AdapterInfo{DevicePath: b.devicePath, LogicalAddresses: []int{}}
	var physical uint16
	if err := b.control(cecAdapGPhysAddr(), unsafe.Pointer(&physical)); err != nil {
		return info, fmt.Errorf("read physical address: %w", err)
	}
	var addresses cecLogAddrs
	if err := b.control(cecAdapGLogAddrs(), unsafe.Pointer(&addresses)); err != nil {
		return info, fmt.Errorf("read logical addresses: %w", err)
	}
	if physical != 0xffff {
		text := physicalAddressText(physical)
		info.PhysicalAddress = &text
	}
	for i := uint8(0); i < addresses.NumLogAddrs && int(i) < len(addresses.LogAddr); i++ {
		address := addresses.LogAddr[i]
		if address < 16 && addresses.LogAddrMask&(1<<address) != 0 {
			info.LogicalAddresses = append(info.LogicalAddresses, int(address))
		}
	}
	return info, nil
}
