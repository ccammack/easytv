package app

import (
	"encoding/hex"

	"github.com/ccammack/easytv/internal/cec"
)

// HandleCEC applies playback policy to a received frame. The serialized owner
// supplies current adapter metadata. It never transmits or rewrites packets.
// A TV selection of our current physical path resumes local playback.
func (a *Coordinator) HandleCEC(message cec.ReceivedMessage, self cec.AdapterInfo) (string, error) {
	if cecSelectsPi(message, self) {
		a.mu.Lock()
		a.sourceAway = false
		a.mu.Unlock()
		return "tv_selected_pi", a.controller.Start()
	}
	reason := cecStopReason(message, self)
	if reason == "" {
		return "", nil
	}
	return reason, a.stop(reason == "other_active_source")
}

func cecSelectsPi(message cec.ReceivedMessage, self cec.AdapterInfo) bool {
	if message.ValidationError != "" || message.Length != 4 || self.PhysicalAddress == nil {
		return false
	}
	raw, err := hex.DecodeString(message.Raw)
	if err != nil || len(raw) != 4 || raw[0] != 0x0f || raw[1] != 0x86 {
		return false
	}
	physical := uint16(raw[2])<<8 | uint16(raw[3])
	if physical == 0 || !validPhysical(physical) || physicalText(physical) != *self.PhysicalAddress {
		return false
	}
	for _, address := range self.LogicalAddresses {
		if address > 0 && address < 15 {
			return true
		}
	}
	return false
}

func cecStopReason(message cec.ReceivedMessage, self cec.AdapterInfo) string {
	if message.ValidationError != "" {
		return ""
	}
	raw, err := hex.DecodeString(message.Raw)
	if err != nil || len(raw) < 2 || len(raw) > 16 || uint32(len(raw)) != message.Length {
		return ""
	}
	initiator, destination := int(raw[0]>>4), int(raw[0]&15)
	own := func(id int) bool {
		for _, address := range self.LogicalAddresses {
			if address >= 0 && address < 15 && address == id {
				return true
			}
		}
		return false
	}
	addressedToUs := destination == 15 || own(destination)
	reason := ""
	switch raw[1] {
	case 0x82: // ACTIVE_SOURCE is a broadcast with a physical address.
		if len(raw) != 4 || destination != 15 || initiator == 15 || own(initiator) {
			return ""
		}
		// Unknown local identity must not be treated as proof of another source.
		knownLogical := false
		for _, id := range self.LogicalAddresses {
			if id >= 0 && id < 15 {
				knownLogical = true
			}
		}
		if !knownLogical || self.PhysicalAddress == nil {
			return ""
		}
		physical := uint16(raw[2])<<8 | uint16(raw[3])
		if !validPhysical(physical) {
			return ""
		}
		text := physicalText(physical)
		if text == *self.PhysicalAddress {
			return ""
		}
		reason = "other_active_source"
	case 0x90: // REPORT_POWER_STATUS from the TV, not another appliance.
		if len(raw) == 3 && initiator == 0 && addressedToUs {
			if raw[2] == 1 {
				reason = "tv_reported_standby"
			}
			if raw[2] == 3 {
				reason = "tv_reported_transition_to_standby"
			}
		}
	case 0x36: // STANDBY is a command, not a power-status report.
		if len(raw) == 2 && initiator == 0 && addressedToUs {
			reason = "tv_standby_command"
		}
	}
	return reason
}

func validPhysical(address uint16) bool {
	if address == 0xffff {
		return false
	}
	zero := false
	for shift := 12; shift >= 0; shift -= 4 {
		component := (address >> uint(shift)) & 15
		if component == 0 {
			zero = true
		} else if zero {
			return false
		}
	}
	return true
}
func physicalText(address uint16) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(address>>12)&15], '.', digits[(address>>8)&15], '.', digits[(address>>4)&15], '.', digits[address&15]})
}
