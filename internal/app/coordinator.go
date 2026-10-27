package app

import (
	"fmt"
	"time"

	"github.com/ccammack/easytv/internal/playback"
)

// Coordinator is called by one serialized application owner, never directly
// from concurrent HTTP handlers. Timer and CEC receivers enqueue work to it.
type Coordinator struct {
	controller *playback.Controller
	preview    *playback.Preview
	claim      playback.ClaimSource
	show       func(string)
}

func New(controller *playback.Controller, preview *playback.Preview, claim playback.ClaimSource, show func(string)) (*Coordinator, error) {
	if controller == nil || preview == nil || claim == nil || show == nil {
		return nil, fmt.Errorf("controller, preview, AS, and OSD callbacks required")
	}
	return &Coordinator{controller: controller, preview: preview, claim: claim, show: show}, nil
}

// RequestChannel asserts AS immediately for each accepted direction request.
// The returned ticket is scheduled by the owner, not by a goroutine here.
func (a *Coordinator) RequestChannel(direction int, now time.Time) (playback.PreviewTicket, error) {
	if direction != 1 && direction != -1 {
		return playback.PreviewTicket{}, fmt.Errorf("direction must be +1 or -1")
	}
	// Validate the base against the preview's channel list before any CEC I/O.
	// A copy permits validation without changing the live preview on AS failure.
	trial := *a.preview
	ticket, err := trial.Request(a.controller.SelectedChannelID(), direction, now)
	if err != nil {
		return playback.PreviewTicket{}, err
	}
	if err := a.claim(); err != nil {
		return playback.PreviewTicket{}, fmt.Errorf("claim active source: %w", err)
	}
	*a.preview = trial
	a.show(ticket.ChannelID)
	return ticket, nil
}

// CommitPreview never reasserts AS: it was already requested by RequestChannel.
// Stop cancels the ticket, preventing a queued timer from restarting playback
// or stealing the TV input after a source-away/standby observation.
func (a *Coordinator) CommitPreview(sequence uint64, now time.Time) error {
	id, ok := a.preview.Commit(sequence, now)
	if !ok {
		return nil
	}
	return a.controller.SelectChannel(id, func() error { return nil })
}
func (a *Coordinator) Stop() error {
	a.preview.Cancel()
	return a.controller.Stop()
}
