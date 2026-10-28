package playback

import (
	"fmt"
	"time"
)

const DefaultPreviewDelay = 2 * time.Second

// PreviewTicket identifies a scheduled commit. Timer callbacks must supply its
// sequence; cancellation alone cannot prevent a callback already queued.
type PreviewTicket struct {
	Sequence  uint64
	ChannelID string
	Due       time.Time
}

// Preview tracks selection only. Its serialized owner supplies the clock,
// schedules timers, displays OSD, and performs CEC/player operations.
type Preview struct {
	ids      []string
	delay    time.Duration
	sequence uint64
	pending  *PreviewTicket
}

func NewPreview(ids []string, delay time.Duration) (*Preview, error) {
	if len(ids) == 0 || delay <= 0 {
		return nil, fmt.Errorf("channels and positive preview delay required")
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, fmt.Errorf("invalid or duplicate channel %q", id)
		}
		seen[id] = true
	}
	return &Preview{ids: append([]string(nil), ids...), delay: delay}, nil
}

// Request moves relative to the pending preview, or current if none exists.
// The application separately gates commits on AS completion. No I/O occurs.
func (p *Preview) Request(current string, direction int, now time.Time) (PreviewTicket, error) {
	if direction != 1 && direction != -1 {
		return PreviewTicket{}, fmt.Errorf("direction must be +1 or -1")
	}
	return p.request(current, direction, now)
}

// Resume schedules the current channel without advancing the selection.
func (p *Preview) Resume(current string, now time.Time) (PreviewTicket, error) {
	return p.request(current, 0, now)
}

func (p *Preview) request(current string, direction int, now time.Time) (PreviewTicket, error) {
	base := current
	if p.pending != nil {
		base = p.pending.ChannelID
	}
	index := -1
	for i, id := range p.ids {
		if id == base {
			index = i
			break
		}
	}
	if index < 0 {
		return PreviewTicket{}, fmt.Errorf("unknown base channel %q", base)
	}
	target := (index + direction + len(p.ids)) % len(p.ids)
	p.sequence++
	ticket := PreviewTicket{Sequence: p.sequence, ChannelID: p.ids[target], Due: now.Add(p.delay)}
	p.pending = &ticket
	return ticket, nil
}

// Commit returns a target exactly once and only for the current, due ticket.
// Returning the current channel is intentional: stopped playback may need to
// start, while an already-running current channel must not be restarted.
func (p *Preview) Commit(sequence uint64, now time.Time) (string, bool) {
	if p.pending == nil || p.pending.Sequence != sequence || now.Before(p.pending.Due) {
		return "", false
	}
	target := p.pending.ChannelID
	p.pending = nil
	return target, true
}

// Cancel must be called on playback stop/source loss to invalidate queued work.
func (p *Preview) Cancel() { p.sequence++; p.pending = nil }
func (p *Preview) Pending() (PreviewTicket, bool) {
	if p.pending == nil {
		return PreviewTicket{}, false
	}
	return *p.pending, true
}

func (p *Preview) Delay() time.Duration { return p.delay }
