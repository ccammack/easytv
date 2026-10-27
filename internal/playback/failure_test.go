package playback

import (
	"errors"
	"testing"

	"github.com/ccammack/easytv/internal/state"
)

func TestMediaFailurePreservesPositionAndRetriesExactFile(t *testing.T) {
	p := &fakePlayer{}
	var persisted state.State
	c := makeController(t, state.Default(), p, func(s state.State) error { persisted = s; return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	old := c.Generation()
	c.RecordPosition(old, 42)
	lost := errors.New("source disconnected")
	if err := c.Fail(old, lost); !errors.Is(err, lost) || c.Running() {
		t.Fatalf("failure=%v running=%v", err, c.Running())
	}
	path, offset, generation := c.RetryTarget()
	if path != "/a/one.mp4" || offset != 39 || c.SelectedChannelID() != "/a" || p.starts != 1 || p.stops != 1 {
		t.Fatalf("target=%s offset=%d player=%+v", path, offset, p)
	}
	if persisted.ChannelState["/a"].OffsetSeconds != 39 {
		t.Fatal("lost resume position")
	}
	if err := c.Fail(old, lost); err != nil || p.stops != 1 {
		t.Fatal("duplicate failure changed playback")
	}
	if err := c.AcceptRetry(path, offset, generation); err != nil || !c.Running() {
		t.Fatalf("adopt retry: %v", err)
	}
	c.RecordPosition(old, 999)
	if c.Snapshot().ChannelState["/a"].OffsetSeconds != 39 {
		t.Fatal("stale observation changed recovered playback")
	}
	if err := c.AcceptRetry(path, offset, generation); err == nil {
		t.Fatal("duplicate retry adoption accepted")
	}
}

func TestAdvanceUnavailableRetainsLastResumePosition(t *testing.T) {
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.RecordPosition(c.Generation(), 42)
	p.startErr = errors.New("disconnected")
	if err := c.Advance(c.Generation()); err == nil || c.Running() {
		t.Fatal("unavailable source did not stop")
	}
	path, offset, _ := c.RetryTarget()
	if path != "/a/one.mp4" || offset != 39 || c.SelectedChannelID() != "/a" {
		t.Fatalf("retry=%s offset=%d", path, offset)
	}
}
