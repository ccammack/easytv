package cec

import (
	"encoding/hex"
	"fmt"
	"time"
)

// ReceivedAt is userspace receipt time, not the kernel monotonic timestamp.
type ReceivedMessage struct {
	ReceivedAt      time.Time      `json:"received_at"`
	Raw             string         `json:"raw"`
	Length          uint32         `json:"length"`
	Initiator       *uint8         `json:"initiator"`
	Destination     *uint8         `json:"destination"`
	Opcode          *uint8         `json:"opcode"`
	MessageName     string         `json:"message_name"`
	Operands        map[string]any `json:"operands,omitempty"`
	ValidationError string         `json:"validation_error,omitempty"`
}

type AdapterInfo struct {
	DevicePath       string  `json:"device_path"`
	LogicalAddresses []int   `json:"logical_addresses"`
	PhysicalAddress  *string `json:"physical_address"`
}

func physicalAddressText(address uint16) string {
	return fmt.Sprintf("%x.%x.%x.%x", (address>>12)&15, (address>>8)&15, (address>>4)&15, address&15)
}

func decodeReceivedMessage(data []byte, reportedLength uint32) ReceivedMessage {
	message := ReceivedMessage{ReceivedAt: time.Now().UTC(), Length: reportedLength, Raw: hex.EncodeToString(data), MessageName: "UNKNOWN"}
	if len(data) > 0 {
		initiator, destination := data[0]>>4, data[0]&15
		message.Initiator, message.Destination = &initiator, &destination
	}
	if len(data) > 1 {
		opcode := data[1]
		message.Opcode = &opcode
	}
	if reportedLength < 1 || reportedLength > 16 || int(reportedLength) != len(data) {
		message.ValidationError = "invalid CEC message length"
		return message
	}
	if len(data) == 1 {
		message.MessageName = "POLL"
		return message
	}
	exact := 0
	switch data[1] {
	case 0x82:
		message.MessageName = "ACTIVE_SOURCE"
		exact = 4
	case 0x86:
		message.MessageName = "SET_STREAM_PATH"
		exact = 4
	case 0x80:
		message.MessageName = "ROUTING_CHANGE"
		exact = 6
	case 0x81:
		message.MessageName = "ROUTING_INFORMATION"
		exact = 4
	case 0x36:
		message.MessageName = "STANDBY"
		exact = 2
	case 0x8f:
		message.MessageName = "GIVE_DEVICE_POWER_STATUS"
		exact = 2
	case 0x90:
		message.MessageName = "REPORT_POWER_STATUS"
		exact = 3
	case 0x85:
		message.MessageName = "REQUEST_ACTIVE_SOURCE"
		exact = 2
	case 0x83:
		message.MessageName = "GIVE_PHYSICAL_ADDRESS"
		exact = 2
	case 0x84:
		message.MessageName = "REPORT_PHYSICAL_ADDRESS"
		exact = 5
	case 0x87:
		message.MessageName = "DEVICE_VENDOR_ID"
		exact = 5
	case 0x00:
		message.MessageName = "FEATURE_ABORT"
		exact = 4
	}
	if exact != 0 && len(data) != exact {
		message.ValidationError = "invalid operand length for " + message.MessageName
		return message
	}
	operands := map[string]any{}
	address := func(offset int) string { return physicalAddressText(uint16(data[offset])<<8 | uint16(data[offset+1])) }
	switch data[1] {
	case 0x82, 0x86, 0x81:
		operands["physical_address"] = address(2)
	case 0x80:
		operands["old_physical_address"], operands["new_physical_address"] = address(2), address(4)
	case 0x84:
		operands["physical_address"], operands["primary_device_type"] = address(2), data[4]
	case 0x90:
		operands["power_status_code"] = data[2]
		names := map[byte]string{0: "on", 1: "standby", 2: "transitioning_to_on", 3: "transitioning_to_standby"}
		if name, ok := names[data[2]]; ok {
			operands["power_status"] = name
		}
	case 0x87:
		operands["vendor_id"] = uint32(data[2])<<16 | uint32(data[3])<<8 | uint32(data[4])
	case 0x00:
		operands["aborted_opcode"], operands["abort_reason"] = data[2], data[3]
	}
	if len(operands) > 0 {
		message.Operands = operands
	}
	return message
}
