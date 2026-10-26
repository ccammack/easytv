//go:build linux

package cec

// This file mirrors the parts of the Linux kernel CEC UAPI header
// (include/uapi/linux/cec.h) that cecmond needs. Field order, widths,
// and array sizes are copied from that header so the Go struct layout
// matches what the kernel expects on /dev/cecX ioctl calls.
//
// If you ever see EINVAL from an ioctl below, the first thing to check
// is that these struct definitions still match /usr/include/linux/cec.h
// on your target kernel (they have been stable for years, but verify).

import "unsafe"

// ---- sizes -----------------------------------------------------------

const (
	cecMaxMsgSize   = 16
	cecMaxLogAddrs  = 4
	cecOSDNameSize  = 15
	cecFeaturesSize = 12
)

// ---- struct cec_msg ----------------------------------------------------
//
// struct cec_msg {
//     __u64 tx_ts;
//     __u64 rx_ts;
//     __u32 len;
//     __u32 timeout;
//     __u32 sequence;
//     __u32 flags;
//     __u8  msg[CEC_MAX_MSG_SIZE];
//     __u8  reply;
//     __u8  rx_status;
//     __u8  tx_status;
//     __u8  tx_arb_lost_cnt;
//     __u8  tx_nack_cnt;
//     __u8  tx_low_drive_cnt;
//     __u8  tx_error_cnt;
// };

type cecMsg struct {
	TxTs          uint64
	RxTs          uint64
	Len           uint32
	Timeout       uint32
	Sequence      uint32
	Flags         uint32
	Msg           [cecMaxMsgSize]byte
	Reply         uint8
	RxStatus      uint8
	TxStatus      uint8
	TxArbLostCnt  uint8
	TxNackCnt     uint8
	TxLowDriveCnt uint8
	TxErrorCnt    uint8
	_             [1]byte // trailing pad so struct size is a multiple of 8
}

// rx_status / tx_status bits
const (
	cecRxStatusOK        = 1 << 0
	cecRxStatusTimeout   = 1 << 1
	cecRxStatusFeatAbort = 1 << 2

	cecTxStatusOK         = 1 << 0
	cecTxStatusArbLost    = 1 << 1
	cecTxStatusNack       = 1 << 2
	cecTxStatusLowDrive   = 1 << 3
	cecTxStatusError      = 1 << 4
	cecTxStatusMaxRetries = 1 << 5
)

func (m *cecMsg) initiator() uint8   { return m.Msg[0] >> 4 }
func (m *cecMsg) destination() uint8 { return m.Msg[0] & 0x0f }
func (m *cecMsg) isBroadcast() bool  { return m.destination() == 0x0f }
func (m *cecMsg) opcode() (uint8, bool) {
	if m.Len < 2 {
		return 0, false
	}
	return m.Msg[1], true
}

func setMsgHeader(m *cecMsg, initiator, destination uint8) {
	m.Msg[0] = (initiator << 4) | (destination & 0x0f)
	m.Len = 1
}

func appendMsgByte(m *cecMsg, b byte) {
	if m.Len < cecMaxMsgSize {
		m.Msg[m.Len] = b
		m.Len++
	}
}

// ---- struct cec_log_addrs ----------------------------------------------
//
// struct cec_log_addrs {
//     __u8  log_addr[CEC_MAX_LOG_ADDRS];
//     __u16 log_addr_mask;
//     __u8  cec_version;
//     __u8  num_log_addrs;
//     __u32 vendor_id;
//     __u32 flags;
//     char  osd_name[15];
//     __u8  primary_device_type[CEC_MAX_LOG_ADDRS];
//     __u8  log_addr_type[CEC_MAX_LOG_ADDRS];
//     __u8  all_device_types[CEC_MAX_LOG_ADDRS];
//     __u8  features[CEC_MAX_LOG_ADDRS][12];
// };

type cecLogAddrs struct {
	LogAddr           [cecMaxLogAddrs]uint8
	LogAddrMask       uint16
	CecVersion        uint8
	NumLogAddrs       uint8
	VendorID          uint32
	Flags             uint32
	OsdName           [cecOSDNameSize]byte
	PrimaryDeviceType [cecMaxLogAddrs]uint8
	LogAddrType       [cecMaxLogAddrs]uint8
	AllDeviceTypes    [cecMaxLogAddrs]uint8
	Features          [cecMaxLogAddrs][cecFeaturesSize]uint8
}

// CEC logical addresses
const (
	cecLogAddrTV               = 0
	cecLogAddrRecord1          = 1
	cecLogAddrRecord2          = 2
	cecLogAddrTuner1           = 3
	cecLogAddrPlayback1        = 4
	cecLogAddrAudioSystem      = 5
	cecLogAddrTuner2           = 6
	cecLogAddrTuner3           = 7
	cecLogAddrPlayback2        = 8
	cecLogAddrRecord3          = 9
	cecLogAddrTuner4           = 10
	cecLogAddrPlayback3        = 11
	cecLogAddrBackup1          = 12
	cecLogAddrBackup2          = 13
	cecLogAddrSpecific         = 14
	cecLogAddrUnregistered     = 15
	cecLogAddrBroadcast        = 15
	cecLogAddrInvalid          = 0xff
	cecLogAddrMaskUnregistered = 1 << 15
)

// primary device types
const (
	cecOpPrimDevtypeTV          = 0
	cecOpPrimDevtypeRecord      = 1
	cecOpPrimDevtypeTuner       = 3
	cecOpPrimDevtypePlayback    = 4
	cecOpPrimDevtypeAudioSystem = 5
	cecOpPrimDevtypeSwitch      = 6
	cecOpPrimDevtypeProcessor   = 7
)

// log_addr_type
const (
	cecLogAddrTypeTV           = 0
	cecLogAddrTypeRecord       = 1
	cecLogAddrTypeTuner        = 2
	cecLogAddrTypePlayback     = 3
	cecLogAddrTypeAudioSystem  = 4
	cecLogAddrTypeSpecific     = 5
	cecLogAddrTypeUnregistered = 6
)

// all_device_types flags (CEC 2.0)
const cecOpAllDevtypePlayback = 1 << 2

const cecVersion14 = 5 // CEC_VERSION_1_4
const cecVersion20 = 6 // CEC_VERSION_2_0

// cec_log_addrs.flags
const cecLogAddrsFlAllowRcPassthru = 1 << 1
const cecLogAddrsFlAllowUnregFallback = 1 << 0
const cecLogAddrsFlCdcOnly = 1 << 2

// ---- struct cec_caps -----------------------------------------------------
//
// struct cec_caps {
//     char  driver[32];
//     char  name[32];
//     __u32 available_log_addrs;
//     __u32 capabilities;
//     __u32 version;
// };

type cecCaps struct {
	Driver            [32]byte
	Name              [32]byte
	AvailableLogAddrs uint32
	Capabilities      uint32
	Version           uint32
}

// capabilities bits
const (
	cecCapPhysAddr      = 1 << 0
	cecCapLogAddrs      = 1 << 1
	cecCapTransmit      = 1 << 2
	cecCapPassthrough   = 1 << 3
	cecCapRC            = 1 << 4
	cecCapMonitorAll    = 1 << 5
	cecCapNeedsHPD      = 1 << 6
	cecCapMonitorPin    = 1 << 7
	cecCapConnectorInfo = 1 << 8
)

func (c cecCaps) canTransmit() bool    { return c.Capabilities&cecCapTransmit != 0 }
func (c cecCaps) needsLogAddrs() bool  { return c.Capabilities&cecCapLogAddrs != 0 }
func (c cecCaps) driverString() string { return cString(c.Driver[:]) }
func (c cecCaps) nameString() string   { return cString(c.Name[:]) }

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// ---- struct cec_event ----------------------------------------------------
//
// struct cec_event_state_change {
//     __u16 phys_addr;
//     __u16 log_addr_mask;
//     __u16 have_conn_info;
// };
// struct cec_event_lost_msgs {
//     __u32 lost_msgs;
// };
// struct cec_event {
//     __u64 ts;
//     __u32 event;
//     __u32 flags;
//     union {
//         struct cec_event_state_change state_change;
//         struct cec_event_lost_msgs lost_msgs;
//         __u32 raw[16];
//     };
// };

type cecEvent struct {
	Ts    uint64
	Event uint32
	Flags uint32
	Raw   [16]uint32 // union storage; largest member (raw[16]) sized to fit all variants
}

func (e *cecEvent) stateChangePhysAddr() uint16 {
	return uint16(e.Raw[0] & 0xffff)
}
func (e *cecEvent) stateChangeLogAddrMask() uint16 {
	return uint16((e.Raw[0] >> 16) & 0xffff)
}

const (
	cecEventStateChange = 1
	cecEventLostMsgs    = 2
	cecEventPinCECLow   = 3
	cecEventPinCECHigh  = 4
	cecEventPinHPDLow   = 5
	cecEventPinHPDHigh  = 6
	cecEventPin5VLow    = 7
	cecEventPin5VHigh   = 8

	cecEventFlInitialState = 1 << 0
)

// ---- opcodes we actually handle ------------------------------------------

const (
	cecMsgActiveSource          = 0x82
	cecMsgImageViewOn           = 0x04
	cecMsgTextViewOn            = 0x0d
	cecMsgStandby               = 0x36
	cecMsgRequestActiveSource   = 0x85
	cecMsgSetStreamPath         = 0x86
	cecMsgRoutingChange         = 0x80
	cecMsgRoutingInformation    = 0x81
	cecMsgGiveDevicePowerStatus = 0x8f
	cecMsgReportPowerStatus     = 0x90
	cecMsgFeatureAbort          = 0x00
	cecMsgCECVersion            = 0x9e
	cecMsgGetCECVersion         = 0x9f
	cecMsgGivePhysicalAddr      = 0x83
	cecMsgReportPhysicalAddr    = 0x84
	cecMsgGiveOSDName           = 0x46
	cecMsgSetOSDName            = 0x47
)

const (
	cecOpPowerStatusOn        = 0
	cecOpPowerStatusStandby   = 1
	cecOpPowerStatusToOn      = 2
	cecOpPowerStatusToStandby = 3
)

// ---- ioctl number computation --------------------------------------------
//
// Mirrors <asm-generic/ioctl.h>:
//   _IOC(dir, type, nr, size) =
//       (dir << 30) | (size << 16) | (type << 8) | nr
// with dir: none=0, write=1, read=2, both=3.
// CEC ioctls use type='a' (0x61).

const (
	iocNone  = 0
	iocWrite = 1
	iocRead  = 2

	iocNrShift   = 0
	iocTypeShift = 8
	iocSizeShift = 16
	iocDirShift  = 30

	cecIOCType = 'a'
)

func ioc(dir, nr uintptr, size uintptr) uintptr {
	return (dir << iocDirShift) | (uintptr(cecIOCType) << iocTypeShift) | (nr << iocNrShift) | (size << iocSizeShift)
}

func iowr(nr uintptr, size uintptr) uintptr { return ioc(iocRead|iocWrite, nr, size) }
func ior(nr uintptr, size uintptr) uintptr  { return ioc(iocRead, nr, size) }
func iow(nr uintptr, size uintptr) uintptr  { return ioc(iocWrite, nr, size) }

var (
	sizeofU32         = unsafe.Sizeof(uint32(0))
	sizeofU16         = unsafe.Sizeof(uint16(0))
	sizeofCecCaps     = unsafe.Sizeof(cecCaps{})
	sizeofCecLogAddrs = unsafe.Sizeof(cecLogAddrs{})
	sizeofCecMsg      = unsafe.Sizeof(cecMsg{})
	sizeofCecEvent    = unsafe.Sizeof(cecEvent{})
)

// Computed lazily (they depend on unsafe.Sizeof of the structs above, which
// Go can only evaluate once the types are known, so these are functions
// rather than package-level ioctl constants).

func cecAdapGCaps() uintptr     { return iowr(0, sizeofCecCaps) }
func cecAdapGPhysAddr() uintptr { return ior(1, sizeofU16) }
func cecAdapSPhysAddr() uintptr { return iow(2, sizeofU16) }
func cecAdapGLogAddrs() uintptr { return ior(3, sizeofCecLogAddrs) }
func cecAdapSLogAddrs() uintptr { return iowr(4, sizeofCecLogAddrs) }
func cecTransmit() uintptr      { return iowr(5, sizeofCecMsg) }
func cecReceive() uintptr       { return iowr(6, sizeofCecMsg) }
func cecDQEvent() uintptr       { return iowr(7, sizeofCecEvent) }
func cecGMode() uintptr         { return ior(8, sizeofU32) }
func cecSMode() uintptr         { return iow(9, sizeofU32) }

// CEC_MODE_* for CEC_S_MODE (initiator role in low byte, follower role in high byte)
const (
	cecModeInitiatorNone = 0x0
	cecModeInitiator     = 0x1
	cecModeExclInitiator = 0x2

	cecModeFollowerNone         = 0x00
	cecModeFollower             = 0x10
	cecModeExclFollower         = 0x20
	cecModeExclFollowerPassthru = 0x30
	cecModeMonitor              = 0xe0
	cecModeMonitorAll           = 0xf0
)
