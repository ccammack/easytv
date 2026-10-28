package app

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestFirstChannelPressReturnsToExistingChannel(t *testing.T) {
	for _, direction := range []int{-1, 1} {
		f := setup(t)
		if reason, err := f.app.HandleCEC(frame("4f822000"), adapter()); err != nil || reason != "other_active_source" {
			t.Fatalf("source away: %q %v", reason, err)
		}
		ticket, err := f.request(direction, time.Now())
		if err != nil || ticket.ChannelID != "a" || f.claims != 1 {
			t.Fatalf("return ticket=%+v claims=%d error=%v", ticket, f.claims, err)
		}
		if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); err != nil {
			t.Fatal(err)
		}
		if !f.controller.Running() || f.controller.SelectedChannelID() != "a" {
			t.Fatal("return changed channel or did not resume")
		}
		next, err := f.request(direction, time.Now())
		want := map[int]string{-1: "c", 1: "b"}[direction]
		if err != nil || next.ChannelID != want {
			t.Fatalf("subsequent preview=%+v want=%s error=%v", next, want, err)
		}
	}
}

func TestRapidPressesAfterSourceAwayAdvanceAfterFirst(t *testing.T) {
	f := setup(t)
	if _, err := f.app.HandleCEC(frame("4f822000"), adapter()); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"a", "b", "c"} {
		if err := f.owner.queueChannel(1, time.Now()); err != nil {
			t.Fatal(err)
		}
		pending, _ := f.app.pending()
		if pending.ChannelID != want {
			t.Fatalf("press %d preview=%s want=%s", i+1, pending.ChannelID, want)
		}
	}
	for range 3 {
		claim := <-f.owner.claims
		f.owner.finishClaim(claim)
	}
	pending, _ := f.app.pending()
	if err := f.app.CommitPreview(pending.Sequence, pending.Due); err != nil {
		t.Fatal(err)
	}
	if f.controller.SelectedChannelID() != "c" {
		t.Fatal("rapid presses were lost")
	}
}

func TestFailedSourceReturnCanBeRetriedWithoutAdvancing(t *testing.T) {
	f := setup(t)
	if _, err := f.app.HandleCEC(frame("4f822000"), adapter()); err != nil {
		t.Fatal(err)
	}
	f.claimErr = errors.New("CEC transmission failed")
	if _, err := f.request(1, time.Now()); err == nil {
		t.Fatal("claim failure lost")
	}
	f.claimErr = nil
	ticket, err := f.request(1, time.Now())
	if err != nil || ticket.ChannelID != "a" {
		t.Fatalf("retry advanced: %+v error=%v", ticket, err)
	}
}

func TestStandbyOrExplicitSelectionDoesNotConsumeChannelPress(t *testing.T) {
	for _, raw := range []string{"0f36", "089001", "0f861000"} {
		f := setup(t)
		if _, err := f.app.HandleCEC(frame("4f822000"), adapter()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.HandleCEC(frame(raw), adapter()); err != nil {
			t.Fatal(err)
		}
		ticket, err := f.request(1, time.Now())
		if err != nil || ticket.ChannelID != "b" {
			t.Fatalf("frame=%s preview=%+v error=%v", raw, ticket, err)
		}
	}
}

func TestRealChannelPressReturnsFromOtherSourceWithoutAdvancing(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	o, ctx, _ := rescanOwner(t, true, nil)
	initial := rescanSubmit(t, o, ctx, OwnerCommand{Kind: "status"})
	if !initial.Running {
		t.Fatal("initial playback not running")
	}
	stopped := rescanSubmit(t, o, ctx, OwnerCommand{
		Kind: "cec", Message: frame("4f822000"), Adapter: adapter(),
	})
	if stopped.Running {
		t.Fatal("source-away did not stop playback")
	}
	quickChannel(t, o, 1)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := rescanSubmit(t, o, ctx, OwnerCommand{Kind: "status"})
		if status.Running && status.Status.PendingChannelID == "" {
			if status.Status.ChannelID != initial.Status.ChannelID || status.Status.FilePath != initial.Status.FilePath {
				t.Fatalf("source return changed playback: %+v", status)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("source return did not resume playback")
}
