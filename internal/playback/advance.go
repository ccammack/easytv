package playback

import (
	"fmt"
	"github.com/ccammack/easytv/internal/state"
)

// Advance handles a terminal event from the current audio/video player.
// The serialized owner must not use this for an image's end-file event.
// Generation checks make duplicate EOF/process-exit notifications harmless.
func (c *Controller) Advance(generation uint64) error {
	if !c.running || generation != c.generation {
		return nil
	}
	c.generation++
	if err := c.player.Stop(); err != nil {
		return fmt.Errorf("stop before media advance: %w", err)
	}
	c.running = false
	ch := c.channels[c.selected]
	next := 0
	old := c.saved.ChannelState[ch.Path]
	if old.FilePath != nil {
		for i, path := range ch.Media {
			if path == *old.FilePath {
				next = (i + 1) % len(ch.Media)
				break
			}
		}
	}
	path := ch.Media[next]
	c.saved.ChannelState[ch.Path] = state.Position{FilePath: &path, OffsetSeconds: 0}
	if err := c.Start(); err != nil {
		return err
	}
	// As in the reference, a persistence failure must not stop new playback.
	if err := c.save(clone(c.saved)); err != nil {
		return fmt.Errorf("save advanced position: %w", err)
	}
	return nil
}
