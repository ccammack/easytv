//go:build linux

package cec

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// LinuxCECBackend talks to a real Linux kernel CEC adapter through the
// /dev/cecX character device using the ioctls defined in
// cec_uapi_linux.go. It implements Backend.
type LinuxCECBackend struct {
	eventReadyFn func() (bool, error)
	ioctlFn      func(int, uintptr, unsafe.Pointer) error
	devicePath   string
	fd           int
	file         *os.File // kept only so Close()/finalizers behave; ioctls use fd directly

	caps          cecCaps
	logAddr       uint8 // startup/lifecycle identity only; commands read live addresses
	haveLog       bool
	ownsLogConfig bool
}

func NewLinuxCECBackend(devicePath string) *LinuxCECBackend {
	return &LinuxCECBackend{devicePath: devicePath, logAddr: cecLogAddrInvalid}
}

// ---- open / close ---------------------------------------------------------

func (b *LinuxCECBackend) Open(devicePath string) error {
	if devicePath != "" {
		b.devicePath = devicePath
	}
	f, err := os.OpenFile(b.devicePath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", b.devicePath, err)
	}
	b.file = f
	b.fd = int(f.Fd())

	if err := b.ioctlGCaps(); err != nil {
		_ = b.Close()
		return fmt.Errorf("CEC_ADAP_G_CAPS: %w", err)
	}
	if !b.caps.canTransmit() {
		_ = b.Close()
		return fmt.Errorf("cec adapter %q does not support CEC_CAP_TRANSMIT", b.devicePath)
	}

	if err := b.ioctlGPhysAddr(); err != nil {
		_ = b.Close()
		return fmt.Errorf("CEC_ADAP_G_PHYS_ADDR: %w", err)
	}

	// We require exclusive initiator mode to transmit, and exclusive follower mode
	// so the kernel core answers generic housekeeping requests (CEC
	// version, physical address, OSD name, etc.) on our behalf and passes
	// everything else to us via CEC_RECEIVE.
	if err := b.ioctlSMode(cecModeExclInitiator | cecModeExclFollower); err != nil {
		_ = b.Close()
		return fmt.Errorf("CEC_S_MODE (exclusive ownership required): %w", err)
	}

	if b.caps.needsLogAddrs() {
		// Configuration is adapter-wide and can survive descriptor closure.
		// Only replace it after acquiring exclusive initiator/follower access.
		if err := b.clearLogicalAddresses(); err != nil {
			_ = b.Close()
			return fmt.Errorf("clear retained logical addresses: %w", err)
		}
		b.ownsLogConfig = true
		if err := b.claimLogicalAddress(); err != nil {
			_ = b.Close()
			return fmt.Errorf("claim logical address: %w", err)
		}
	} else {
		// Some adapters (rare) auto-configure logical addresses; read
		// back whatever the kernel already assigned.
		if err := b.ioctlGLogAddrs(); err != nil {
			_ = b.Close()
			return fmt.Errorf("CEC_ADAP_G_LOG_ADDRS: %w", err)
		}
	}

	return nil
}

func (b *LinuxCECBackend) Close() error {
	var clearErr, closeErr error
	if b.file != nil {
		if b.ownsLogConfig {
			if err := b.clearLogicalAddresses(); err != nil {
				clearErr = fmt.Errorf("clear logical addresses on close: %w", err)
			}
		}
		closeErr = b.file.Close()
	}
	b.file = nil
	b.fd = -1
	b.ownsLogConfig = false
	b.haveLog = false
	b.logAddr = cecLogAddrInvalid
	return errors.Join(clearErr, closeErr)
}

func (b *LinuxCECBackend) clearLogicalAddresses() error {
	var addresses cecLogAddrs
	return b.control(cecAdapSLogAddrs(), unsafe.Pointer(&addresses))
}

// ---- capability / address setup -------------------------------------------

func (b *LinuxCECBackend) ioctlGCaps() error {
	var caps cecCaps
	if err := b.control(cecAdapGCaps(), unsafe.Pointer(&caps)); err != nil {
		return err
	}
	b.caps = caps
	return nil
}

func (b *LinuxCECBackend) ioctlGPhysAddr() error {
	var pa uint16
	if err := b.control(cecAdapGPhysAddr(), unsafe.Pointer(&pa)); err != nil {
		return err
	}
	return nil
}

func (b *LinuxCECBackend) ioctlSMode(mode uint32) error {
	return b.control(cecSMode(), unsafe.Pointer(&mode))
}

func (b *LinuxCECBackend) ioctlGLogAddrs() error {
	var la cecLogAddrs
	if err := b.control(cecAdapGLogAddrs(), unsafe.Pointer(&la)); err != nil {
		return err
	}
	if la.NumLogAddrs == 0 {
		b.logAddr = cecLogAddrInvalid
		b.haveLog = false
		return nil
	}
	b.logAddr = la.LogAddr[0]
	b.haveLog = true
	return nil
}

// claimLogicalAddress asks the kernel to claim a logical address for us
// as a playback device (the role cecmond is designed for: a media player
// sitting on the bus). The kernel handles the actual CEC polling/
// arbitration and reports back which address it won.
func (b *LinuxCECBackend) claimLogicalAddress() error {
	var la cecLogAddrs
	la.CecVersion = cecVersion14
	la.NumLogAddrs = 1
	la.VendorID = 0xffffffff // CEC_VENDOR_ID_NONE
	la.Flags = cecLogAddrsFlAllowRcPassthru

	name := "cecmond"
	copy(la.OsdName[:], name)

	la.PrimaryDeviceType[0] = cecOpPrimDevtypePlayback
	la.LogAddrType[0] = cecLogAddrTypePlayback
	la.AllDeviceTypes[0] = cecOpAllDevtypePlayback
	// la.LogAddr[0] left as 0 (CEC_LOG_ADDR_INVALID would be 0xff, but the
	// kernel wants CEC_LOG_ADDR_UNREGISTERED-style "let me pick" behavior
	// driven by log_addr_type/primary_device_type, not log_addr itself).
	la.LogAddr[0] = cecLogAddrInvalid

	if err := b.control(cecAdapSLogAddrs(), unsafe.Pointer(&la)); err != nil {
		return err
	}

	if la.NumLogAddrs == 0 || la.LogAddr[0] == cecLogAddrInvalid {
		return errors.New("kernel did not assign a logical address (bus may be full or no TV present)")
	}

	b.logAddr = la.LogAddr[0]
	b.haveLog = true
	return nil
}

// ---- CECBackend interface ---------------------------------------------

func (b *LinuxCECBackend) SelfID() (int, error) {
	if !b.haveLog {
		return 0, errors.New("no logical address claimed yet")
	}
	return int(b.logAddr), nil
}

func (b *LinuxCECBackend) SetPower(on bool) error {
	logical, err := b.currentTransmitLogicalAddress()
	if err != nil {
		return err
	}
	var m cecMsg
	if on {
		setMsgHeader(&m, logical, cecLogAddrTV)
		appendMsgByte(&m, cecMsgImageViewOn)
	} else {
		setMsgHeader(&m, logical, cecLogAddrBroadcast)
		appendMsgByte(&m, cecMsgStandby)
	}
	m.Timeout = 1000
	return b.transmit(&m)
}

func (b *LinuxCECBackend) ClaimActiveSource() error {
	logical, err := b.currentTransmitLogicalAddress()
	if err != nil {
		return err
	}
	physical, err := b.currentTransmitPhysicalAddress()
	if err != nil {
		return err
	}
	var m cecMsg
	setMsgHeader(&m, logical, cecLogAddrBroadcast)
	appendMsgByte(&m, cecMsgActiveSource)
	appendMsgByte(&m, byte(physical>>8))
	appendMsgByte(&m, byte(physical))
	m.Timeout = 1000
	return b.transmit(&m)
}

func (b *LinuxCECBackend) transmit(m *cecMsg) error {
	if err := b.control(cecTransmit(), unsafe.Pointer(m)); err != nil {
		return err
	}
	if m.TxStatus&cecTxStatusOK == 0 {
		return fmt.Errorf("cec transmit failed: tx_status=0x%02x", m.TxStatus)
	}
	return nil
}

// Poll returns one received packet or a separate kernel adapter event.
// Receive waits at most 500ms so the caller can observe shutdown.
// Packets are decoded without inferring source or power state.
func (b *LinuxCECBackend) Poll() (*ReceivedMessage, *AdapterEvent, error) {
	if diagnostic, err := b.dequeueEvent(); err != nil || diagnostic != nil {
		return nil, diagnostic, err
	}
	var m cecMsg
	m.Timeout = 500
	if err := b.control(cecReceive(), unsafe.Pointer(&m)); err != nil {
		if errors.Is(err, syscall.ETIMEDOUT) || errors.Is(err, syscall.EAGAIN) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	length := m.Len
	if length > uint32(len(m.Msg)) {
		length = uint32(len(m.Msg))
	}
	message := decodeReceivedMessage(m.Msg[:int(length)], m.Len)
	return &message, nil, nil
}

// eventReady checks exceptional readiness (CEC adapter events) without waiting.
// Keeping the descriptor blocking preserves existing transmit/receive semantics.
func (b *LinuxCECBackend) eventReady() (bool, error) {
	if b.eventReadyFn != nil {
		return b.eventReadyFn()
	}
	var exceptional syscall.FdSet
	bits := int(unsafe.Sizeof(exceptional.Bits[0]) * 8)
	if b.fd < 0 || b.fd >= len(exceptional.Bits)*bits {
		return false, fmt.Errorf("CEC event readiness: fd %d outside select range", b.fd)
	}
	exceptional.Bits[b.fd/bits] |= 1 << uint(b.fd%bits)
	timeout := syscall.Timeval{}
	n, err := syscall.Select(b.fd+1, nil, nil, &exceptional, &timeout)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (b *LinuxCECBackend) dequeueEvent() (*AdapterEvent, error) {
	ready, err := b.eventReady()
	if err != nil {
		return nil, fmt.Errorf("CEC event readiness: %w", err)
	}
	if !ready {
		return nil, nil
	}
	var ev cecEvent
	if err := b.control(cecDQEvent(), unsafe.Pointer(&ev)); err != nil {
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENODATA) {
			return nil, nil
		}
		return nil, fmt.Errorf("CEC_DQEVENT: %w", err)
	}
	diagnostic := AdapterEvent{ReceivedAt: time.Now().UTC(), KernelTimestampNS: ev.Ts, Event: ev.Event, Flags: ev.Flags, Raw: ev.Raw}
	switch ev.Event {
	case cecEventStateChange:
		address := ev.stateChangePhysAddr()
		text := physicalAddressText(address)
		if address != 0xffff {
			diagnostic.PhysicalAddress = &text
		}
		mask := ev.stateChangeLogAddrMask()
		diagnostic.LogicalAddressMask = &mask
	case cecEventLostMsgs:
		count := ev.Raw[0]
		diagnostic.LostMessages = &count
	}
	return &diagnostic, nil
}

// ---- raw ioctl helper ------------------------------------------------

// ioctl performs a Linux ioctl(2) call and turns a nonzero errno into a Go
// error. syscall.Syscall is used directly (instead of golang.org/x/sys/unix)
// so this file has zero external module dependencies.
func ioctl(fd int, cmd uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), cmd, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// control allows tests to substitute adapter syscalls.
func (b *LinuxCECBackend) control(cmd uintptr, arg unsafe.Pointer) error {
	if b.ioctlFn != nil {
		return b.ioctlFn(b.fd, cmd, arg)
	}
	return ioctl(b.fd, cmd, arg)
}
