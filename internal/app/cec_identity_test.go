package app

import (
	"testing"

	"github.com/ccammack/easytv/internal/cec"
)

func metadata(logical int, physical string) cec.AdapterInfo {
	return cec.AdapterInfo{LogicalAddresses: []int{logical}, PhysicalAddress: &physical}
}

func TestCECUsesNewLogicalAndPhysicalAddresses(t *testing.T) {
	f := setup(t)
	if reason, err := f.app.HandleCEC(frame("8f821000"), metadata(8, "1.0.0.0")); reason != "" || err != nil {
		t.Fatal(reason, err)
	}
	current := metadata(11, "3.0.0.0")
	if reason, err := f.app.HandleCEC(frame("bf823000"), current); reason != "" || err != nil || !f.controller.Running() {
		t.Fatal("new self address not recognized")
	}
	reason, err := f.app.HandleCEC(frame("8f821000"), current)
	if err != nil || reason != "other_active_source" || f.controller.Running() {
		t.Fatal(reason, err)
	}
}

func TestCECTargetsNewLogicalAddressForPowerReport(t *testing.T) {
	f := setup(t)
	if _, err := f.app.HandleCEC(frame("089000"), metadata(8, "1.0.0.0")); err != nil {
		t.Fatal(err)
	}
	current := metadata(11, "3.0.0.0")
	if reason, err := f.app.HandleCEC(frame("089001"), current); reason != "" || err != nil {
		t.Fatal("report to old address acted on")
	}
	if reason, err := f.app.HandleCEC(frame("0b9001"), current); reason != "tv_reported_standby" || err != nil || f.controller.Running() {
		t.Fatal(reason, err)
	}
}

func TestCECTemporaryAddressLossDoesNotUseOldIdentity(t *testing.T) {
	f := setup(t)
	if _, err := f.app.HandleCEC(frame("8f821000"), metadata(8, "1.0.0.0")); err != nil {
		t.Fatal(err)
	}
	if reason, err := f.app.HandleCEC(frame("4f822000"), cec.AdapterInfo{}); reason != "" || err != nil || !f.controller.Running() {
		t.Fatal("unassigned identity replaced by stale address")
	}
	if reason, err := f.app.HandleCEC(frame("4f822000"), metadata(4, "2.0.0.0")); reason != "" || err != nil {
		t.Fatal("new identity not recognized")
	}
}
