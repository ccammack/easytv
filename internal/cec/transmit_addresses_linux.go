//go:build linux

package cec

import (
	"errors"
	"fmt"
	"unsafe"
)

// currentTransmitLogicalAddress reads current kernel metadata without changing
// startup bookkeeping. It sends no CEC query and does not alter configuration.
func (b *LinuxCECBackend) currentTransmitLogicalAddress() (uint8, error) {
	var addresses cecLogAddrs
	if err := b.control(cecAdapGLogAddrs(), unsafe.Pointer(&addresses)); err != nil {
		return 0, fmt.Errorf("read current transmit logical addresses: %w", err)
	}
	if addresses.NumLogAddrs > uint8(len(addresses.LogAddr)) {
		return 0, errors.New("invalid current logical-address count")
	}
	for i := uint8(0); i < addresses.NumLogAddrs; i++ {
		address := addresses.LogAddr[i]
		if address < cecLogAddrUnregistered && addresses.LogAddrMask&(uint16(1)<<address) != 0 {
			return address, nil
		}
	}
	return 0, errors.New("no registered logical address currently claimed")
}

// currentTransmitPhysicalAddress validates only outgoing address preparation.
// Received packet operands and /adapter metadata remain unmodified.
func (b *LinuxCECBackend) currentTransmitPhysicalAddress() (uint16, error) {
	var address uint16
	if err := b.control(cecAdapGPhysAddr(), unsafe.Pointer(&address)); err != nil {
		return 0, fmt.Errorf("read current transmit physical address: %w", err)
	}
	if address == 0xffff {
		return 0, errors.New("no physical address currently available")
	}
	// Once a hierarchy component is zero, all following components must be zero.
	zeroSeen := false
	for shift := 12; shift >= 0; shift -= 4 {
		component := (address >> uint(shift)) & 15
		if component == 0 {
			zeroSeen = true
		} else if zeroSeen {
			return 0, fmt.Errorf("invalid current physical address %s", physicalAddressText(address))
		}
	}
	return address, nil
}
