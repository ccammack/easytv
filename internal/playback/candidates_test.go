package playback

import (
	"errors"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/state"
	"reflect"
	"testing"
)

type candidatePlayer struct {
	paths   []string
	offsets []int64
	fail    map[string]error
	stops   int
}

func (p *candidatePlayer) Start(path string, offset int64, _ uint64) error {
	p.paths = append(p.paths, path)
	p.offsets = append(p.offsets, offset)
	return p.fail[path]
}
func (p *candidatePlayer) Stop() error { p.stops++; return nil }

func TestStartSkipsFailedCandidatesAndKeepsActualPath(t *testing.T) {
	failure := errors.New("unreadable first item")
	first := "/a/one.mp4"
	saved := state.Default()
	saved.ChannelState["/a"] = state.Position{FilePath: &first, OffsetSeconds: 17}
	p := &candidatePlayer{fail: map[string]error{first: failure}}
	c, err := New(channels(), saved, p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.paths, []string{first, "/a/two.mp4"}) || !reflect.DeepEqual(p.offsets, []int64{17, 17}) {
		t.Fatalf("paths=%v offsets=%v", p.paths, p.offsets)
	}
	actual := c.Snapshot().ChannelState["/a"]
	if actual.FilePath == nil || *actual.FilePath != "/a/two.mp4" || !c.Running() {
		t.Fatal("actual candidate not recorded")
	}
}

func TestAllCandidatesFailOnceWithoutStateMutation(t *testing.T) {
	first, second := errors.New("first failure"), errors.New("second failure")
	saved := state.Default()
	p := &candidatePlayer{fail: map[string]error{"/a/one.mp4": first, "/a/two.mp4": second}}
	c, err := New(channels(), saved, p, func(state.State) error { t.Fatal("failed startup saved state"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	err = c.Start()
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatal(err)
	}
	if len(p.paths) != 2 || c.Running() || !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("unbounded attempt or failed startup state mutation")
	}
	delete(p.fail, "/a/two.mp4")
	if err := c.Start(); err != nil || !c.Running() {
		t.Fatalf("retry failed: %v", err)
	}
}

func TestAdvanceSkipsUnplayableNextItem(t *testing.T) {
	p := &candidatePlayer{fail: map[string]error{"/a/two.mp4": errors.New("bad next item")}}
	saves := 0
	c, err := New(channels(), state.Default(), p, func(state.State) error { saves++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(c.Generation()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.paths, []string{"/a/one.mp4", "/a/two.mp4", "/a/one.mp4"}) || p.stops != 1 || saves != 1 {
		t.Fatalf("paths=%v stops=%d saves=%d", p.paths, p.stops, saves)
	}
}

func TestSavedImageFallbackStillStartsAudioAtZero(t *testing.T) {
	photo := "/a/photo.jpg"
	saved := state.Default()
	saved.ChannelState["/a"] = state.Position{FilePath: &photo, OffsetSeconds: 90}
	p := &candidatePlayer{fail: map[string]error{photo: errors.New("bad image")}}
	c, err := New([]catalog.Channel{{Path: "/a", Media: []string{photo, "/a/sound.wav"}}}, saved, p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.offsets, []int64{0, 0}) {
		t.Fatal(p.offsets)
	}
}
