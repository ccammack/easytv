package app

import (
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/cec"
)

func adapter() cec.AdapterInfo {
	physical := "1.0.0.0"
	return cec.AdapterInfo{LogicalAddresses: []int{8}, PhysicalAddress: &physical}
}
func frame(raw string) cec.ReceivedMessage {
	data, _ := hex.DecodeString(raw)
	return cec.ReceivedMessage{Raw: raw, Length: uint32(len(data))}
}
func TestCECStopDecisionsCancelPreview(t *testing.T) {
	cases := map[string]string{
		"4f822000": "other_active_source",
		"089001":   "tv_reported_standby",
		"0f9003":   "tv_reported_transition_to_standby",
		"0f36":     "tv_standby_command",
		"0836":     "tv_standby_command",
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			f := setup(t)
			ticket, err := f.request(1, time.Unix(100, 0))
			if err != nil {
				t.Fatal(err)
			}
			reason, err := f.app.HandleCEC(frame(raw), adapter())
			if err != nil || reason != want {
				t.Fatalf("reason=%q err=%v", reason, err)
			}
			if f.controller.Running() || f.player.stops != 1 || f.saves != 1 {
				t.Fatal("CEC did not save-stop")
			}
			if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); err != nil {
				t.Fatal(err)
			}
			if f.player.starts != 1 || f.claims != 1 {
				t.Fatal("CEC stop allowed queued preview restart or AS")
			}
			if _, err := f.app.HandleCEC(frame(raw), adapter()); err != nil {
				t.Fatal(err)
			}
			if f.player.stops != 1 || f.saves != 1 {
				t.Fatal("repeat observation performed repeated stop")
			}
		})
	}
}
func TestCECUnrelatedOwnAndMalformedFramesDoNothing(t *testing.T) {
	frames := []string{
		"8f821000",             // own announcement
		"4f821000",             // same physical path: not proof of source away
		"48822000",             // AS not broadcast
		"ff822000",             // unregistered initiator
		"4f82ffff", "4f821020", // invalid physical paths
		"489001", "4f36", // not the TV
		"009001", "0036", // not addressed to us or broadcast
		"089000", "089002", "0890ff", // not TV standby
		"0f862000", // selection of another input does not resume us
		"0f82", "0890", "0f3600", "zz", "", "04",
	}
	for _, raw := range frames {
		t.Run(raw, func(t *testing.T) {
			f := setup(t)
			reason, err := f.app.HandleCEC(frame(raw), adapter())
			if err != nil || reason != "" || !f.controller.Running() || f.saves != 0 || f.player.stops != 0 || f.claims != 0 {
				t.Fatalf("reason=%q err=%v", reason, err)
			}
		})
	}
}

func TestTVSelectionResumesWithoutClaimOrRestart(t *testing.T) {
	f := setup(t)
	if _, err := f.app.HandleCEC(frame("0f36"), adapter()); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"089000", "0f862000", "4f861000", "08861000", "0f86100000"} {
		reason, err := f.app.HandleCEC(frame(raw), adapter())
		if err != nil || reason != "" || f.controller.Running() {
			t.Fatalf("unrelated frame %s resumed: reason=%q err=%v", raw, reason, err)
		}
	}
	for range 2 {
		reason, err := f.app.HandleCEC(frame("0f861000"), adapter())
		if err != nil || reason != "tv_selected_pi" || !f.controller.Running() {
			t.Fatalf("TV selection did not resume: reason=%q err=%v", reason, err)
		}
	}
	if f.player.starts != 2 || f.claims != 0 {
		t.Fatal("duplicate selection restarted playback or transmitted AS")
	}
}

func TestTVSelectionRequiresCurrentValidIdentity(t *testing.T) {
	for _, self := range []cec.AdapterInfo{{}, metadata(15, "1.0.0.0"), metadata(8, "2.0.0.0")} {
		if cecSelectsPi(frame("0f861000"), self) {
			t.Fatal("unknown or different identity accepted")
		}
	}
	packet := frame("0f861000")
	packet.ValidationError = "invalid"
	if cecSelectsPi(packet, adapter()) || cecSelectsPi(frame("0f860000"), metadata(8, "0.0.0.0")) {
		t.Fatal("invalid TV selection accepted")
	}
	if !cecSelectsPi(frame("0f863000"), metadata(11, "3.0.0.0")) {
		t.Fatal("updated adapter identity ignored")
	}
}
func TestCECRequiresValidFrameAndKnownSourceIdentity(t *testing.T) {
	for _, self := range []cec.AdapterInfo{{}, {LogicalAddresses: []int{8}}, {LogicalAddresses: []int{15}}} {
		f := setup(t)
		reason, err := f.app.HandleCEC(frame("4f822000"), self)
		if err != nil || reason != "" || !f.controller.Running() {
			t.Fatal("unknown identity treated as another source")
		}
	}
	for _, malformed := range []bool{false, true} {
		f := setup(t)
		packet := frame("4f822000")
		if malformed {
			packet.ValidationError = "invalid"
		} else {
			packet.Length = 3
		}
		reason, err := f.app.HandleCEC(packet, adapter())
		if err != nil || reason != "" || !f.controller.Running() {
			t.Fatal("invalid frame triggered stop")
		}
	}
}
func TestCECReportsSaveFailureButStillStops(t *testing.T) {
	f := setup(t)
	failure := errors.New("disk failed")
	f.saveErr = failure
	reason, err := f.app.HandleCEC(frame("089001"), adapter())
	if reason != "tv_reported_standby" || !errors.Is(err, failure) || f.controller.Running() || f.player.stops != 1 {
		t.Fatalf("reason=%q err=%v", reason, err)
	}
}
