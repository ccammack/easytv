package playback

import (
	"testing"
	"time"
)

func previewForTest(t *testing.T) *Preview {
	t.Helper()
	p, err := NewPreview([]string{"a", "b", "c"}, DefaultPreviewDelay)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPreviewWaitsUntilDueAndCommitsOnce(t *testing.T) {
	p := previewForTest(t)
	now := time.Unix(100, 0)
	ticket, err := p.Request("a", 1, now)
	if err != nil || ticket.ChannelID != "b" || !ticket.Due.Equal(now.Add(2*time.Second)) {
		t.Fatalf("ticket=%+v err=%v", ticket, err)
	}
	if _, ok := p.Commit(ticket.Sequence, ticket.Due.Add(-time.Nanosecond)); ok {
		t.Fatal("committed early")
	}
	if target, ok := p.Commit(ticket.Sequence, ticket.Due); !ok || target != "b" {
		t.Fatal("due commit failed")
	}
	if _, ok := p.Commit(ticket.Sequence, ticket.Due); ok {
		t.Fatal("duplicate commit")
	}
	if _, ok := p.Pending(); ok {
		t.Fatal("pending retained")
	}
}
func TestRapidPreviewUsesPendingTargetAndResetsDeadline(t *testing.T) {
	p := previewForTest(t)
	now := time.Unix(100, 0)
	first, err := p.Request("a", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Request("a", 1, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.ChannelID != "c" || !second.Due.Equal(now.Add(3*time.Second)) {
		t.Fatal(second)
	}
	if _, ok := p.Commit(first.Sequence, now.Add(10*time.Second)); ok {
		t.Fatal("stale ticket committed")
	}
	if _, ok := p.Commit(second.Sequence, first.Due); ok {
		t.Fatal("old deadline used")
	}
	if id, ok := p.Commit(second.Sequence, second.Due); !ok || id != "c" {
		t.Fatal("latest target lost")
	}
}
func TestPreviewWrapsBackToCurrent(t *testing.T) {
	p := previewForTest(t)
	now := time.Unix(100, 0)
	var ticket PreviewTicket
	for i := 0; i < 3; i++ {
		var err error
		ticket, err = p.Request("a", 1, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if id, ok := p.Commit(ticket.Sequence, ticket.Due); !ok || id != "a" {
		t.Fatal("wrapped target incorrect")
	}
	ticket, err := p.Request("a", -1, now)
	if err != nil || ticket.ChannelID != "c" {
		t.Fatal("previous wrap failed")
	}
}
func TestCancelInvalidatesQueuedCommitAndResetsBase(t *testing.T) {
	p := previewForTest(t)
	now := time.Unix(100, 0)
	old, err := p.Request("a", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	p.Cancel()
	p.Cancel()
	if _, ok := p.Commit(old.Sequence, old.Due); ok {
		t.Fatal("cancelled commit accepted")
	}
	next, err := p.Request("a", 1, now)
	if err != nil || next.ChannelID != "b" || next.Sequence <= old.Sequence {
		t.Fatal("cancel did not reset preview")
	}
}
func TestInvalidRequestDoesNotAlterPending(t *testing.T) {
	p := previewForTest(t)
	now := time.Unix(100, 0)
	ticket, err := p.Request("a", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Request("a", 0, now); err == nil {
		t.Fatal("invalid direction accepted")
	}
	got, ok := p.Pending()
	if !ok || got != ticket {
		t.Fatal("invalid request changed pending ticket")
	}
	p.Cancel()
	if _, err := p.Request("missing", 1, now); err == nil {
		t.Fatal("unknown current accepted")
	}
}
func TestPreviewOwnsIDsAndReturnsTicketCopies(t *testing.T) {
	ids := []string{"a", "b"}
	p, err := NewPreview(ids, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ids[1] = "changed"
	ticket, err := p.Request("a", 1, time.Unix(100, 0))
	if err != nil || ticket.ChannelID != "b" {
		t.Fatal("IDs not copied")
	}
	ticket.ChannelID = "changed"
	got, ok := p.Pending()
	if !ok || got.ChannelID != "b" {
		t.Fatal("ticket not copied")
	}
}
func TestInvalidPreviewConstruction(t *testing.T) {
	for _, ids := range [][]string{nil, {""}, {"a", "a"}} {
		if _, err := NewPreview(ids, time.Second); err == nil {
			t.Fatal("accepted invalid IDs")
		}
	}
	for _, delay := range []time.Duration{0, -time.Second} {
		if _, err := NewPreview([]string{"a"}, delay); err == nil {
			t.Fatal("accepted invalid delay")
		}
	}
}
