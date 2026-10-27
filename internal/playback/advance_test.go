package playback

import (
	"errors"
	"github.com/ccammack/easytv/internal/state"
	"testing"
)

func TestAdvanceWrapsAndIgnoresDuplicateTerminalEvents(t *testing.T) {
	p := &fakePlayer{}
	saves := 0
	c := makeController(t, state.Default(), p, func(state.State) error { saves++; return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/a/two.mp4", "/a/one.mp4", "/a/two.mp4"} {
		old := c.Generation()
		c.RecordPosition(old, 43)
		if err := c.Advance(old); err != nil {
			t.Fatal(err)
		}
		if p.path != want || p.offset != 0 || !c.Running() {
			t.Fatalf("path=%q offset=%d running=%v", p.path, p.offset, c.Running())
		}
		starts, stops := p.starts, p.stops
		if err := c.Advance(old); err != nil {
			t.Fatal(err)
		}
		c.RecordPosition(old, 100)
		if p.starts != starts || p.stops != stops || c.Snapshot().ChannelState["/a"].OffsetSeconds != 0 {
			t.Fatal("stale terminal/position changed new playback")
		}
	}
	if saves != 4 {
		t.Fatalf("saves=%d want=4", saves)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	starts := p.starts
	if err := c.Advance(c.Generation()); err != nil || p.starts != starts {
		t.Fatal("stopped player advanced")
	}
}

func TestAdvanceStopFailureDoesNotLaunchReplacement(t *testing.T) {
	failure := errors.New("stop failed")
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	old := c.Generation()
	p.stopErr = failure
	if err := c.Advance(old); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if p.starts != 1 || !c.Running() {
		t.Fatal("stop failure launched replacement")
	}
	p.stopErr = nil
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceSaveFailureKeepsNewPlaybackRunning(t *testing.T) {
	failure := errors.New("disk failed")
	p := &fakePlayer{}
	failSave := false
	c := makeController(t, state.Default(), p, func(state.State) error {
		if failSave {
			return failure
		}
		return nil
	})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	failSave = true
	if err := c.Advance(c.Generation()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if !c.Running() || p.path != "/a/two.mp4" || p.starts != 2 || p.stops != 1 {
		t.Fatal("persistence failure interrupted advancement")
	}
}

func TestAdvanceStartFailureRemainsStoppedAndRetryable(t *testing.T) {
	failure := errors.New("start failed")
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	p.startErr = failure
	if err := c.Advance(c.Generation()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if c.Running() {
		t.Fatal("failed start marked running")
	}
	p.startErr = nil
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if p.path != "/a/two.mp4" || p.offset != 0 {
		t.Fatal("retry lost next item")
	}
}
