package playback

import (
	"errors"
	"fmt"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/state"
)

func (c *Controller) ChannelIDs() []string {
	ids := make([]string, len(c.channels))
	for i, channel := range c.channels {
		ids[i] = channel.Path
	}
	return ids
}

// ReplaceChannels is called only by the playback owner. Validate before
// stopping, preserve the latest position, and never wake stopped playback.
func (c *Controller) ReplaceChannels(channels []catalog.Channel) error {
	next, err := New(channels, c.Snapshot(), c.player, c.save)
	if err != nil {
		return err
	}
	selected := c.SelectedChannelID()
	index := make(map[string]int, len(channels))
	for i, channel := range channels {
		index[channel.Path] = i
		if _, ok := next.saved.ChannelState[channel.Path]; !ok {
			next.saved.ChannelState[channel.Path] = state.Position{}
		}
	}
	if i, ok := index[selected]; ok {
		next.selected = i
	} else {
		// Prefer the next surviving channel in the established catalog order.
		for step := 1; step < len(c.channels); step++ {
			id := c.channels[(c.selected+step)%len(c.channels)].Path
			if i, ok := index[id]; ok {
				next.selected = i
				break
			}
		}
	}
	running := c.running
	stopErr := c.Stop()
	if c.running {
		return fmt.Errorf("stop before catalog replacement: %w", stopErr)
	}
	next.generation = c.generation
	*c = *next
	id := c.SelectedChannelID()
	c.saved.CurrentChannelID = &id
	if !running {
		return errors.Join(stopErr, c.save(c.Snapshot()))
	}
	var failures []error
	for attempt := 0; attempt < len(c.channels); attempt++ {
		err := c.Start()
		if c.running {
			return errors.Join(stopErr, errors.Join(failures...), err)
		}
		failures = append(failures, err)
		if attempt+1 < len(c.channels) {
			c.selected = (c.selected + 1) % len(c.channels)
			id := c.SelectedChannelID()
			c.saved.CurrentChannelID = &id
		}
	}
	return errors.Join(stopErr, errors.Join(failures...), c.save(c.Snapshot()))
}
