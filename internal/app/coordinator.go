package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/ccammack/easytv/internal/playback"
)

// Controller operations have one serialized owner. Preview state has its own
// lock so feedback can continue during CEC transmission and player startup.
type Coordinator struct {
	mu         sync.Mutex
	selected   string
	epoch      uint64
	controller *playback.Controller
	preview    *playback.Preview
	claim      playback.ClaimSource
	show       func(string)
}

func New(controller *playback.Controller, preview *playback.Preview, claim playback.ClaimSource, show func(string)) (*Coordinator, error) {
	if controller == nil || preview == nil || claim == nil || show == nil {
		return nil, fmt.Errorf("controller, preview, AS, and OSD callbacks required")
	}
	return &Coordinator{controller: controller, preview: preview, claim: claim, show: show, selected: controller.SelectedChannelID()}, nil
}

// CommitPreview never reasserts AS: the owner waits for the queued claim first.
// Stop cancels the ticket, preventing a queued timer from restarting playback
// or stealing the TV input after a source-away/standby observation.
func (a *Coordinator) CommitPreview(sequence uint64, now time.Time) error {
	a.mu.Lock()
	id, ok := a.preview.Commit(sequence, now)
	if ok {
		a.selected = id
	}
	a.mu.Unlock()
	if !ok {
		return nil
	}
	err := a.controller.SelectChannel(id, func() error { return nil })
	a.mu.Lock()
	a.selected = a.controller.SelectedChannelID()
	a.mu.Unlock()
	return err
}
func (a *Coordinator) Stop() error {
	a.cancelPreview()
	return a.controller.Stop()
}

func (a *Coordinator) pending() (playback.PreviewTicket, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.preview.Pending()
}

func (a *Coordinator) cancelPreview() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.epoch++
	a.preview.Cancel()
	a.show("")
}

func (a *Coordinator) replacePreview() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	preview, err := playback.NewPreview(a.controller.ChannelIDs(), a.preview.Delay())
	if err != nil {
		return err
	}
	a.preview = preview
	a.selected = a.controller.SelectedChannelID()
	a.epoch++
	a.show("")
	return nil
}

func (a *Coordinator) showIfNoPreview(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.preview.Pending(); !ok {
		a.show(text)
	}
}
