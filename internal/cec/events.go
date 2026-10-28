package cec

import "time"

// AdapterEvent is kernel adapter metadata, not a received CEC packet.
// It is logged separately from playback observations.
type AdapterEvent struct {
	ReceivedAt         time.Time  `json:"received_at"`
	KernelTimestampNS  uint64     `json:"kernel_timestamp_ns"`
	Event              uint32     `json:"event"`
	Flags              uint32     `json:"flags"`
	Raw                [16]uint32 `json:"raw"`
	PhysicalAddress    *string    `json:"physical_address,omitempty"`
	LogicalAddressMask *uint16    `json:"logical_address_mask,omitempty"`
	LostMessages       *uint32    `json:"lost_messages,omitempty"`
}
