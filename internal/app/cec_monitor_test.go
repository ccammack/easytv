package app

import (
	"errors"
	"testing"

	"github.com/ccammack/easytv/internal/cec"
)

type metadataReader struct {
	info  cec.AdapterInfo
	err   error
	reads int
}

func (r *metadataReader) AdapterInfo() (cec.AdapterInfo, error) { r.reads++; return r.info, r.err }
func metadata(logical int, physical string) cec.AdapterInfo {
	return cec.AdapterInfo{LogicalAddresses: []int{logical}, PhysicalAddress: &physical}
}
func TestMonitorUsesNewLogicalAndPhysicalAddresses(t *testing.T) {
	f := setup(t)
	reader := &metadataReader{info: metadata(8, "1.0.0.0")}
	monitor, err := NewCECMonitor(f.app, reader)
	if err != nil {
		t.Fatal(err)
	}
	if reader.reads != 0 {
		t.Fatal("construction read adapter")
	}
	if reason, err := monitor.Handle(frame("8f821000")); reason != "" || err != nil {
		t.Fatal(reason, err)
	}
	reader.info = metadata(11, "3.0.0.0")
	if reason, err := monitor.Handle(frame("bf823000")); reason != "" || err != nil || !f.controller.Running() {
		t.Fatal("new self address not recognized")
	}
	reason, err := monitor.Handle(frame("8f821000"))
	if err != nil || reason != "other_active_source" || f.controller.Running() {
		t.Fatal(reason, err)
	}
	if reader.reads != 3 {
		t.Fatalf("metadata reads=%d", reader.reads)
	}
}
func TestMonitorTargetsNewLogicalAddressForPowerReport(t *testing.T) {
	f := setup(t)
	reader := &metadataReader{info: metadata(8, "1.0.0.0")}
	monitor, err := NewCECMonitor(f.app, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := monitor.Handle(frame("089000")); err != nil {
		t.Fatal(err)
	}
	reader.info = metadata(11, "3.0.0.0")
	if reason, err := monitor.Handle(frame("089001")); reason != "" || err != nil {
		t.Fatal("report to old address acted on")
	}
	if reason, err := monitor.Handle(frame("0b9001")); reason != "tv_reported_standby" || err != nil || f.controller.Running() {
		t.Fatal(reason, err)
	}
}
func TestReadFailureDoesNotReusePreviousIdentity(t *testing.T) {
	f := setup(t)
	reader := &metadataReader{info: metadata(8, "1.0.0.0")}
	monitor, err := NewCECMonitor(f.app, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := monitor.Handle(frame("8f821000")); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("adapter unavailable")
	reader.err = failure
	reason, err := monitor.Handle(frame("4f822000"))
	if reason != "" || !errors.Is(err, failure) {
		t.Fatal(reason, err)
	}
	if !f.controller.Running() || f.player.stops != 0 {
		t.Fatal("failed read used stale identity")
	}
	reader.err = nil
	reader.info = metadata(11, "3.0.0.0")
	if reason, err := monitor.Handle(frame("bf823000")); reason != "" || err != nil {
		t.Fatal("recovery failed")
	}
	if reader.reads != 3 {
		t.Fatalf("metadata reads=%d", reader.reads)
	}
}
func TestTemporaryAddressLossDoesNotUseOldIdentity(t *testing.T) {
	f := setup(t)
	reader := &metadataReader{info: metadata(8, "1.0.0.0")}
	monitor, err := NewCECMonitor(f.app, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := monitor.Handle(frame("8f821000")); err != nil {
		t.Fatal(err)
	}
	reader.info = cec.AdapterInfo{}
	if reason, err := monitor.Handle(frame("4f822000")); reason != "" || err != nil || !f.controller.Running() {
		t.Fatal("unassigned identity replaced by stale address")
	}
	reader.info = metadata(4, "2.0.0.0")
	if reason, err := monitor.Handle(frame("4f822000")); reason != "" || err != nil {
		t.Fatal("new identity not recognized")
	}
}
func TestInvalidMonitorConstruction(t *testing.T) {
	f := setup(t)
	reader := &metadataReader{}
	if _, err := NewCECMonitor(nil, reader); err == nil {
		t.Fatal("nil coordinator accepted")
	}
	if _, err := NewCECMonitor(f.app, nil); err == nil {
		t.Fatal("nil reader accepted")
	}
}
