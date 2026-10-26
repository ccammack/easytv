package playback

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/state"
)

type fakePlayer struct {
	starts, stops     int
	path              string
	offset            int64
	generation        uint64
	startErr, stopErr error
	order             *[]string
}

func (p *fakePlayer) Start(path string, offset int64, generation uint64) error {
	p.starts++
	p.path = path
	p.offset = offset
	p.generation = generation
	return p.startErr
}
func (p *fakePlayer) Stop() error {
	p.stops++
	if p.order != nil {
		*p.order = append(*p.order, "stop")
	}
	return p.stopErr
}
func channels() []catalog.Channel {
	return []catalog.Channel{{Path: "/a", Media: []string{"/a/one.mp4", "/a/two.mp4"}}, {Path: "/b", Media: []string{"/b/video.mp4"}}}
}
func makeController(t *testing.T, s state.State, p *fakePlayer, save SaveFunc) *Controller {
	t.Helper()
	c, err := New(channels(), s, p, save)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestSaveStopAndRestart(t *testing.T) {
	p := &fakePlayer{}
	var saves []state.State
	var order []string
	p.order = &order
	c := makeController(t, state.Default(), p, func(s state.State) error { order = append(order, "save"); saves = append(saves, s); return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	gen := c.Generation()
	c.RecordPosition(gen, 42)
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if c.Running() || p.stops != 1 || len(saves) != 1 || saves[0].ChannelState["/a"].OffsetSeconds != 39 {
		t.Fatalf("saves=%v stops=%d", saves, p.stops)
	}
	if !reflect.DeepEqual(order, []string{"save", "stop"}) {
		t.Fatal(order)
	}
	c.RecordPosition(gen, 999)
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || p.stops != 1 {
		t.Fatal("repeated stop performed work")
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if p.offset != 39 {
		t.Fatalf("offset=%d", p.offset)
	}
	c.RecordPosition(gen, 999)
	if c.Snapshot().ChannelState["/a"].OffsetSeconds != 39 {
		t.Fatal("stale event accepted")
	}
	if err := c.Start(); err != nil || p.starts != 2 {
		t.Fatal("start is not idempotent")
	}
}
func TestRestoreAndMissingFile(t *testing.T) {
	for _, missing := range []bool{false, true} {
		s := state.Default()
		id := "/b"
		path := "/b/video.mp4"
		if missing {
			path = "/b/deleted.mp4"
		}
		s.CurrentChannelID = &id
		s.ChannelState[id] = state.Position{FilePath: &path, OffsetSeconds: 50}
		p := &fakePlayer{}
		c := makeController(t, s, p, func(state.State) error { return nil })
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		want := int64(50)
		if missing {
			want = 0
		}
		if p.path != "/b/video.mp4" || p.offset != want {
			t.Fatalf("player=%+v", p)
		}
	}
}
func TestSaveFailureStillStops(t *testing.T) {
	failure := errors.New("disk failed")
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return failure })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); !errors.Is(err, failure) || p.stops != 1 || c.Running() {
		t.Fatalf("err=%v player=%+v", err, p)
	}
}
func TestStopFailureCanBeRetried(t *testing.T) {
	failure := errors.New("stop failed")
	p := &fakePlayer{stopErr: failure}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); !errors.Is(err, failure) || !c.Running() {
		t.Fatal("failed stop not retained")
	}
	p.stopErr = nil
	if err := c.Stop(); err != nil || c.Running() || p.stops != 2 {
		t.Fatal("retry failed")
	}
}
func TestFailedStartDoesNotOverwriteState(t *testing.T) {
	failure := errors.New("start failed")
	p := &fakePlayer{startErr: failure}
	s := state.Default()
	c := makeController(t, s, p, func(state.State) error { t.Fatal("unexpected save"); return nil })
	if err := c.Start(); !errors.Is(err, failure) || c.Running() {
		t.Fatal("start failure lost")
	}
	if !reflect.DeepEqual(c.Snapshot(), s) {
		t.Fatal("failed start overwrote state")
	}
}
func TestSnapshotsAreIndependent(t *testing.T) {
	p := &fakePlayer{}
	c := makeController(t, state.Default(), p, func(state.State) error { return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	*s.CurrentChannelID = "changed"
	pos := s.ChannelState["/a"]
	*pos.FilePath = "changed"
	delete(s.ChannelState, "/a")
	got := c.Snapshot()
	if *got.CurrentChannelID != "/a" || *got.ChannelState["/a"].FilePath != "/a/one.mp4" {
		t.Fatal("snapshot mutated controller")
	}
}
func TestInvalidConstruction(t *testing.T) {
	p := &fakePlayer{}
	save := func(state.State) error { return nil }
	for _, list := range [][]catalog.Channel{nil, {{Path: "/a"}}, {{Path: "/a", Media: []string{""}}}, {{Path: "/a", Media: []string{"x"}}, {Path: "/a", Media: []string{"y"}}}} {
		if _, err := New(list, state.Default(), p, save); err == nil {
			t.Fatal("accepted invalid channels")
		}
	}
	if _, err := New(channels(), state.Default(), nil, save); err == nil {
		t.Fatal("accepted nil player")
	}
	if _, err := New(channels(), state.Default(), p, nil); err == nil {
		t.Fatal("accepted nil save")
	}
}
