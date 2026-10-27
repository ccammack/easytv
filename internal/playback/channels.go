package playback

import (
	"errors"
	"fmt"
)

// ClaimSource is supplied by the application CEC owner. These methods execute
// committed channel actions; preview/debounce scheduling belongs above them.
type ClaimSource func() error

func (c *Controller) SelectedChannelID() string { return c.channels[c.selected].Path }

// SelectChannel asserts AS on every valid request, including a request for the
// already-selected channel. It never starts a second player while one remains.
func (c *Controller) SelectChannel(id string, claim ClaimSource) error {
	target := -1
	for i, ch := range c.channels {
		if ch.Path == id {
			target = i
			break
		}
	}
	if target < 0 {
		return fmt.Errorf("unknown channel %q", id)
	}
	if claim == nil {
		return fmt.Errorf("active-source operation required")
	}
	if err := claim(); err != nil {
		return fmt.Errorf("claim active source: %w", err)
	}
	if target == c.selected {
		return c.Start()
	}
	stopErr := c.Stop()
	// Only a child-stop failure blocks replacement; failed persistence must
	// remain visible without turning a successful stop into a failed switch.
	if c.running {
		return fmt.Errorf("stop before channel change: %w", stopErr)
	}
	c.selected = target
	return errors.Join(stopErr, c.Start())
}

func (c *Controller) NextChannel(claim ClaimSource) error {
	target := (c.selected + 1) % len(c.channels)
	return c.SelectChannel(c.channels[target].Path, claim)
}
func (c *Controller) PreviousChannel(claim ClaimSource) error {
	target := (c.selected + len(c.channels) - 1) % len(c.channels)
	return c.SelectChannel(c.channels[target].Path, claim)
}
