package playback

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ccammack/easytv/internal/state"
)

func TestChannelSwitchSavesAndRestoresPositions(t *testing.T) {
	p := &fakePlayer{}
	var saves []state.State
	c := makeController(t, state.Default(), p, func(s state.State) error { saves = append(saves, s); return nil })
	claims := 0
	claim := func() error { claims++; return nil }
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.RecordPosition(c.Generation(), 43)
	if err := c.NextChannel(claim); err != nil {
		t.Fatal(err)
	}
	if c.SelectedChannelID() != "/b" || p.path != "/b/video.mp4" || p.offset != 0 || p.stops != 1 || claims != 1 {
		t.Fatalf("player=%+v claims=%d", p, claims)
	}
	if len(saves) != 1 || saves[0].ChannelState["/a"].OffsetSeconds != 40 {
		t.Fatal(saves)
	}
	c.RecordPosition(c.Generation(), 23)
	if err := c.PreviousChannel(claim); err != nil {
		t.Fatal(err)
	}
	if p.path != "/a/one.mp4" || p.offset != 40 || claims != 2 {
		t.Fatalf("player=%+v claims=%d", p, claims)
	}
	if c.Snapshot().ChannelState["/b"].OffsetSeconds != 20 {
		t.Fatal("lost channel B position")
	}
}

func TestSameChannelAlwaysClaimsWithoutRestart(t *testing.T) {
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	claims := 0
	claim := func() error { claims++; return nil }
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := c.SelectChannel("/a", claim); err != nil {
			t.Fatal(err)
		}
	}
	if claims != 3 || p.starts != 1 || p.stops != 0 {
		t.Fatalf("claims=%d player=%+v", claims, p)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectChannel("/a", claim); err != nil {
		t.Fatal(err)
	}
	if claims != 4 || p.starts != 2 || !c.Running() {
		t.Fatal("same channel did not restart stopped playback")
	}
}

func TestChannelWrapsInBothDirections(t *testing.T) {
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	claim := func() error { return nil }
	if err := c.PreviousChannel(claim); err != nil {
		t.Fatal(err)
	}
	if c.SelectedChannelID() != "/b" {
		t.Fatal("previous did not wrap")
	}
	if err := c.NextChannel(claim); err != nil {
		t.Fatal(err)
	}
	if c.SelectedChannelID() != "/a" {
		t.Fatal("next did not wrap")
	}
}

func TestRejectedChannelCommandDoesNotDisturbPlayback(t *testing.T) {
	failure := errors.New("CEC failed")
	for _, scenario := range []string{"unknown", "nil claim", "claim failure"} {
		t.Run(scenario, func(t *testing.T) {
			p := &fakePlayer{}
			c := makeController(t, state.Default(), p, func(state.State) error { t.Fatal("unexpected save"); return nil })
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			before := c.Snapshot()
			id := "/b"
			calls := 0
			claim := ClaimSource(func() error { calls++; return failure })
			if scenario == "unknown" {
				id = "/missing"
			}
			if scenario == "nil claim" {
				claim = nil
			}
			err := c.SelectChannel(id, claim)
			if err == nil {
				t.Fatal("accepted invalid command")
			}
			if scenario == "claim failure" && !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if scenario == "unknown" && calls != 0 {
				t.Fatal("unknown channel asserted AS")
			}
			if p.starts != 1 || p.stops != 0 || !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatal("rejected command changed playback")
			}
		})
	}
}

func TestStopFailurePreventsNewPlayer(t *testing.T) {
	failure := errors.New("stop failed")
	p := &fakePlayer{stopErr: failure}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	err := c.NextChannel(func() error { return nil })
	if !errors.Is(err, failure) || p.starts != 1 || c.SelectedChannelID() != "/a" {
		t.Fatalf("err=%v player=%+v", err, p)
	}
}

func TestSaveFailureAbortsSwitchAfterStopping(t *testing.T) {
	failure := errors.New("save failed")
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return failure })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	err := c.NextChannel(func() error { return nil })
	if !errors.Is(err, failure) || c.Running() || p.starts != 1 || p.stops != 1 || c.SelectedChannelID() != "/a" {
		t.Fatalf("err=%v player=%+v", err, p)
	}
}

func TestTargetStartFailureRemainsStoppedAndRetryable(t *testing.T) {
	failure := errors.New("start failed")
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	p.startErr = failure
	if err := c.NextChannel(func() error { return nil }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if c.Running() || c.SelectedChannelID() != "/b" {
		t.Fatal("failed start state incorrect")
	}
	p.startErr = nil
	if err := c.SelectChannel("/b", func() error { return nil }); err != nil || !c.Running() {
		t.Fatal("retry failed")
	}
}
