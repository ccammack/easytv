package playback

import (
	"errors"
	"fmt"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/state"
)

// Player methods complete their operation before returning. Stop must wait for
// the child process to exit. The controller has one serialized caller.
type Player interface {
	Start(path string, offsetSeconds int64, generation uint64) error
	Stop() error
}
type SaveFunc func(state.State) error

type Controller struct {
	channels   []catalog.Channel
	saved      state.State
	player     Player
	save       SaveFunc
	selected   int
	running    bool
	generation uint64
}

func clone(s state.State) state.State {
	result := s
	if s.CurrentChannelID != nil {
		v := *s.CurrentChannelID
		result.CurrentChannelID = &v
	}
	result.ChannelState = make(map[string]state.Position, len(s.ChannelState))
	for id, p := range s.ChannelState {
		if p.FilePath != nil {
			v := *p.FilePath
			p.FilePath = &v
		}
		result.ChannelState[id] = p
	}
	return result
}
func New(channels []catalog.Channel, s state.State, p Player, save SaveFunc) (*Controller, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if p == nil || save == nil {
		return nil, errors.New("player and save function required")
	}
	if len(channels) == 0 {
		return nil, errors.New("no channels")
	}
	c := &Controller{saved: clone(s), player: p, save: save}
	seen := make(map[string]bool)
	for i, ch := range channels {
		if ch.Path == "" || len(ch.Media) == 0 || seen[ch.Path] {
			return nil, fmt.Errorf("invalid or duplicate channel %q", ch.Path)
		}
		for _, path := range ch.Media {
			if path == "" {
				return nil, errors.New("empty media path")
			}
		}
		seen[ch.Path] = true
		ch.Media = append([]string(nil), ch.Media...)
		c.channels = append(c.channels, ch)
		if s.CurrentChannelID != nil && ch.Path == *s.CurrentChannelID {
			c.selected = i
		}
	}
	return c, nil
}
func (c *Controller) Snapshot() state.State { return clone(c.saved) }
func (c *Controller) Running() bool         { return c.running }
func (c *Controller) Generation() uint64    { return c.generation }

// Start launches the selected channel at its saved file and offset.
// Unknown saved paths fall back to the first file with offset zero.
func (c *Controller) Start() error {
	if c.running {
		return nil
	}
	ch := c.channels[c.selected]
	path, offset := ch.Media[0], int64(0)
	old := c.saved.ChannelState[ch.Path]
	if old.FilePath != nil {
		for _, candidate := range ch.Media {
			if candidate == *old.FilePath {
				path = candidate
				offset = old.OffsetSeconds
				break
			}
		}
	}
	c.generation++
	if err := c.player.Start(path, offset, c.generation); err != nil {
		return fmt.Errorf("start player: %w", err)
	}
	id := ch.Path
	c.saved.CurrentChannelID = &id
	c.saved.ChannelState[id] = state.Position{FilePath: &path, OffsetSeconds: offset}
	c.running = true
	return nil
}

// RecordPosition ignores events from old players and keeps a three-second
// resume rewind. Callers pass only audio/video positions, never image timers.
func (c *Controller) RecordPosition(generation uint64, seconds int64) {
	if !c.running || generation != c.generation || seconds < 0 {
		return
	}
	offset := seconds - 3
	if offset < 0 {
		offset = 0
	}
	id := c.channels[c.selected].Path
	p := c.saved.ChannelState[id]
	p.OffsetSeconds = offset
	c.saved.ChannelState[id] = p
}

// Stop saves the known position, invalidates player events, and stops playback.
// A save failure never prevents attempting to stop. Repeated successful stops
// do nothing; a failed player stop leaves a retryable running state.
func (c *Controller) Stop() error {
	if !c.running {
		return nil
	}
	saveErr := c.save(clone(c.saved))
	c.generation++
	stopErr := c.player.Stop()
	if stopErr == nil {
		c.running = false
	}
	return errors.Join(saveErr, stopErr)
}
