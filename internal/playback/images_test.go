package playback

import (
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/state"
	"testing"
)

func TestImageResumeAndPositionRemainZero(t *testing.T) {
	path := "/images/photo.JPG"
	saved := state.Default()
	saved.ChannelState["/images"] = state.Position{FilePath: &path, OffsetSeconds: 90}
	p := &fakePlayer{}
	c, err := New([]catalog.Channel{{Path: "/images", Media: []string{path}}}, saved, p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if p.offset != 0 {
		t.Fatalf("image started at %d", p.offset)
	}
	c.RecordPosition(c.Generation(), 100)
	if got := c.Snapshot().ChannelState["/images"].OffsetSeconds; got != 0 {
		t.Fatalf("image offset=%d", got)
	}
}
