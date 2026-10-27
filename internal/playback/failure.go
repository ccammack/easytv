package playback

import (
	"errors"
	"fmt"

	"github.com/ccammack/easytv/internal/state"
)

// Fail preserves the selected file/position rather than interpreting a read
// error as EOF. Recovery scheduling belongs to the application owner.
func (c *Controller) Fail(generation uint64, cause error) error {
	if !c.running || generation != c.generation {
		return nil
	}
	stopErr := c.Stop()
	return errors.Join(
		fmt.Errorf("media unavailable in channel %q: %w", c.SelectedChannelID(), cause),
		stopErr,
	)
}

// RetryTarget selects only the saved file, never another file or channel.
func (c *Controller) RetryTarget() (string, int64, uint64) {
	channel := c.channels[c.selected]
	position := c.saved.ChannelState[channel.Path]
	path := channel.Media[0]
	if position.FilePath != nil {
		path = *position.FilePath
	}
	return path, position.OffsetSeconds, c.generation + 1
}

// AcceptRetry adopts a player started by the recovery worker after that
// worker has finished. The owner cancels/joins it before any other start.
func (c *Controller) AcceptRetry(path string, offset int64, generation uint64) error {
	if c.running || generation != c.generation+1 {
		return errors.New("stale playback retry")
	}
	c.generation = generation
	channel := c.channels[c.selected]
	id := channel.Path
	c.saved.CurrentChannelID = &id
	c.saved.ChannelState[id] = state.Position{FilePath: &path, OffsetSeconds: offset}
	c.running = true
	return c.save(c.Snapshot())
}
