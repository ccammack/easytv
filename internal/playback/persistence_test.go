package playback

import (
	"errors"
	"github.com/ccammack/easytv/internal/state"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStartPersistsRestoredOffsetBeforeStop(t *testing.T) {
	id, path := "/b", "/b/video.mp4"
	saved := state.Default()
	saved.CurrentChannelID = &id
	saved.ChannelState[id] = state.Position{FilePath: &path, OffsetSeconds: 50}
	destination := filepath.Join(t.TempDir(), "state.json")
	p := &fakePlayer{}
	c := makeController(t, saved, p, func(s state.State) error { return state.Save(destination, s) })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	disk, err := state.Load(destination)
	if err != nil {
		t.Fatal(err)
	}
	if disk.CurrentChannelID == nil || *disk.CurrentChannelID != id || disk.ChannelState[id].FilePath == nil || *disk.ChannelState[id].FilePath != path || disk.ChannelState[id].OffsetSeconds != 50 || p.offset != 50 || p.stops != 0 {
		t.Fatalf("disk=%+v player=%+v", disk, p)
	}
	// Reconstruct from disk before any graceful stop or position observation.
	replacement := &fakePlayer{}
	restored := makeController(t, disk, replacement, func(state.State) error { return nil })
	if err := restored.Start(); err != nil {
		t.Fatal(err)
	}
	if replacement.path != path || replacement.offset != 50 {
		t.Fatal("startup lost resume offset before first position event")
	}
}

func TestChannelSwitchPersistsTargetBeforeStop(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "state.json")
	writes := 0
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(s state.State) error { writes++; return state.Save(destination, s) })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.RecordPosition(c.Generation(), 43)
	if err := c.NextChannel(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	disk, err := state.Load(destination)
	if err != nil {
		t.Fatal(err)
	}
	if disk.CurrentChannelID == nil || *disk.CurrentChannelID != "/b" || disk.ChannelState["/a"].OffsetSeconds != 40 || disk.ChannelState["/b"].FilePath == nil || *disk.ChannelState["/b"].FilePath != "/b/video.mp4" || writes != 3 || p.stops != 1 {
		t.Fatalf("disk=%+v writes=%d", disk, writes)
	}
	if err := c.SelectChannel("/b", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if writes != 3 || p.starts != 2 {
		t.Fatal("idempotent start performed an extra save or launch")
	}
	replacement := &fakePlayer{}
	restored := makeController(t, disk, replacement, func(state.State) error { return nil })
	if err := restored.Start(); err != nil {
		t.Fatal(err)
	}
	if restored.SelectedChannelID() != "/b" || replacement.path != "/b/video.mp4" {
		t.Fatal("restart did not select persisted target")
	}
}

func TestStartSaveFailureKeepsOwnedPlayerRunning(t *testing.T) {
	failure := errors.New("storage unavailable")
	writes := 0
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { writes++; return failure })
	if err := c.Start(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if !c.Running() || p.starts != 1 || p.stops != 0 || writes != 1 {
		t.Fatal("save failure changed player ownership")
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if p.starts != 1 || writes != 1 {
		t.Fatal("save failure caused another launch or implicit retry")
	}
}

func TestSwitchPreservesBothSaveAndStopFailures(t *testing.T) {
	saveFailure, stopFailure := errors.New("save failed"), errors.New("child stop failed")
	failSave := false
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error {
		if failSave {
			return saveFailure
		}
		return nil
	})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	failSave = true
	p.stopErr = stopFailure
	err := c.NextChannel(func() error { return nil })
	if !errors.Is(err, saveFailure) || !errors.Is(err, stopFailure) || !c.Running() || c.SelectedChannelID() != "/a" || p.starts != 1 {
		t.Fatalf("err=%v player=%+v", err, p)
	}
}

func TestFallbackPersistsOnlyActualSuccessfulCandidate(t *testing.T) {
	first := "/a/one.mp4"
	saved := state.Default()
	saved.ChannelState["/a"] = state.Position{FilePath: &first, OffsetSeconds: 17}
	p := &candidatePlayer{fail: map[string]error{first: errors.New("unreadable")}}
	var persisted []state.State
	c, err := New(channels(), saved, p, func(s state.State) error { persisted = append(persisted, s); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].ChannelState["/a"].FilePath == nil || *persisted[0].ChannelState["/a"].FilePath != "/a/two.mp4" || persisted[0].ChannelState["/a"].OffsetSeconds != 17 {
		t.Fatal(persisted)
	}
	if !reflect.DeepEqual(p.offsets, []int64{17, 17}) {
		t.Fatal(p.offsets)
	}
}

func TestStartSaveReceivesIndependentSnapshot(t *testing.T) {
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(s state.State) error {
		*s.CurrentChannelID = "mutated"
		*s.ChannelState["/a"].FilePath = "mutated"
		delete(s.ChannelState, "/a")
		return nil
	})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	got := c.Snapshot()
	if got.CurrentChannelID == nil || *got.CurrentChannelID != "/a" || got.ChannelState["/a"].FilePath == nil || *got.ChannelState["/a"].FilePath != "/a/one.mp4" {
		t.Fatal("save callback mutated controller")
	}
}
