package playback

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/state"
)

func TestReplaceChannelsPreservesLatestPositionAndStoppedState(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "playing", true: "stopped"}[stopped], func(t *testing.T) {
			p := &fakePlayer{}
			var persisted state.State
			c := makeController(t, state.Default(), p, func(s state.State) error { persisted = s; return nil })
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			oldGeneration := c.Generation()
			c.RecordPosition(oldGeneration, 42)
			if stopped {
				if err := c.Stop(); err != nil {
					t.Fatal(err)
				}
			}
			updated := append([]catalog.Channel{{Path: "/0", Media: []string{"/0/one.mp4"}}}, channels()...)
			updated[1].Media = append(updated[1].Media, "/a/new.mp4")
			if err := c.ReplaceChannels(updated); err != nil {
				t.Fatal(err)
			}
			if c.Running() == stopped || p.stops != 1 || p.starts != map[bool]int{false: 2, true: 1}[stopped] {
				t.Fatalf("running=%v player=%+v", c.Running(), p)
			}
			if persisted.ChannelState["/a"].OffsetSeconds != 39 || p.offset != map[bool]int64{false: 39, true: 0}[stopped] {
				t.Fatalf("persisted=%+v player=%+v", persisted, p)
			}
			c.RecordPosition(oldGeneration, 999)
			if c.Snapshot().ChannelState["/a"].OffsetSeconds != 39 {
				t.Fatal("stale player position accepted after replacement")
			}
		})
	}
}

func TestReplaceChannelsRemovedSelectionAndFile(t *testing.T) {
	list := []catalog.Channel{
		{Path: "/a", Media: []string{"/a/one.mp4"}},
		{Path: "/b", Media: []string{"/b/one.mp4"}},
		{Path: "/c", Media: []string{"/c/one.mp4"}},
	}
	for _, test := range []struct {
		name, selected, want, file string
		replacement                []catalog.Channel
	}{
		{"next surviving", "/b", "/c", "/c/one.mp4", []catalog.Channel{list[0], list[2]}},
		{"wrap", "/c", "/a", "/a/one.mp4", list[:2]},
		{"all replaced", "/b", "/new", "/new/one.mp4", []catalog.Channel{{Path: "/new", Media: []string{"/new/one.mp4"}}}},
		{"file removed", "/b", "/b", "/b/new.mp4", []catalog.Channel{{Path: "/b", Media: []string{"/b/new.mp4"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := state.Default()
			s.CurrentChannelID = &test.selected
			path := test.selected + "/one.mp4"
			s.ChannelState[test.selected] = state.Position{FilePath: &path, OffsetSeconds: 30}
			p := &fakePlayer{}
			c, err := New(list, s, p, func(state.State) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			if err := c.ReplaceChannels(test.replacement); err != nil {
				t.Fatal(err)
			}
			if c.SelectedChannelID() != test.want || p.path != test.file || p.offset != 0 || !c.Running() {
				t.Fatalf("selected=%s player=%+v", c.SelectedChannelID(), p)
			}
		})
	}
}

func TestReplaceChannelsFailures(t *testing.T) {
	p := &fakePlayer{}
	failSave := false
	failure := errors.New("save failed")
	c := makeController(t, state.Default(), p, func(state.State) error {
		if failSave {
			return failure
		}
		return nil
	})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.ReplaceChannels(nil); err == nil || p.stops != 0 || !c.Running() {
		t.Fatal("invalid replacement stopped playback")
	}
	p.stopErr = errors.New("stop failed")
	if err := c.ReplaceChannels(channels()[1:]); !errors.Is(err, p.stopErr) || c.SelectedChannelID() != "/a" || p.starts != 1 {
		t.Fatal("stop failure replaced or overlapped playback")
	}
	p.stopErr = nil
	failSave = true
	if err := c.ReplaceChannels(channels()[1:]); !errors.Is(err, failure) || !c.Running() || c.SelectedChannelID() != "/b" {
		t.Fatal("save failure prevented successful replacement or was hidden")
	}
}

func TestReplaceChannelsSkipsUnplayableChannel(t *testing.T) {
	failure := errors.New("unplayable")
	p := &candidatePlayer{fail: map[string]error{}}
	c, err := New(channels(), state.Default(), p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	p.fail["/a/one.mp4"], p.fail["/a/two.mp4"] = failure, failure
	if err := c.ReplaceChannels(channels()); !errors.Is(err, failure) {
		t.Fatalf("fallback failure not reported: %v", err)
	}
	if !c.Running() || c.SelectedChannelID() != "/b" || !reflect.DeepEqual(p.paths, []string{"/a/one.mp4", "/a/one.mp4", "/a/two.mp4", "/b/video.mp4"}) {
		t.Fatalf("selected=%s paths=%v", c.SelectedChannelID(), p.paths)
	}
	p.fail["/b/video.mp4"] = failure
	if err := c.ReplaceChannels(channels()); !errors.Is(err, failure) || c.Running() {
		t.Fatalf("all failed: %v running=%v", err, c.Running())
	}
}
