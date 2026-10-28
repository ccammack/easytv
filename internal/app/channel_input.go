package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ccammack/easytv/internal/playback"
)

type channelClaim struct {
	ticket playback.PreviewTicket
	epoch  uint64
	err    error
}

// Preview state and OSD are independent of the playback owner. Only the owner
// touches the controller; one bounded worker serializes all channel AS claims.
func (o *Owner) queueChannel(direction int, now time.Time) error {
	a := o.app
	a.mu.Lock()
	defer a.mu.Unlock()
	if !o.accepting {
		return errors.New("owner stopped")
	}
	if direction != 1 && direction != -1 {
		return errors.New("direction must be +1 or -1")
	}
	trial := *a.preview
	var ticket playback.PreviewTicket
	var err error
	if _, pending := trial.Pending(); a.sourceAway && !pending {
		ticket, err = trial.Resume(a.selected, now)
	} else {
		ticket, err = trial.Request(a.selected, direction, now)
	}
	if err != nil {
		return err
	}
	select {
	case o.claims <- channelClaim{ticket: ticket, epoch: a.epoch}:
		*a.preview = trial
		a.show(ticket.ChannelID)
		select {
		case o.inputWake <- struct{}{}:
		default:
		}
		return nil
	default:
		return errors.New("channel input queue full")
	}
}

func (o *Owner) finishClaim(claim channelClaim) (playback.PreviewTicket, bool) {
	a := o.app
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.epoch == claim.epoch && claim.err == nil {
		a.sourceAway = false
	}
	pending, ok := a.preview.Pending()
	if !ok || a.epoch != claim.epoch || pending.Sequence != claim.ticket.Sequence {
		return playback.PreviewTicket{}, false
	}
	if claim.err != nil {
		a.epoch++
		a.preview.Cancel()
		a.show("")
	}
	return pending, true
}

func (o *Owner) claimCurrent(claim channelClaim) bool {
	o.app.mu.Lock()
	defer o.app.mu.Unlock()
	return o.app.epoch == claim.epoch
}

func (o *Owner) runClaims(ctx context.Context) {
	defer close(o.claimsDone)
	for {
		select {
		case <-ctx.Done():
			return
		case claim := <-o.claims:
			if ctx.Err() != nil {
				return
			}
			if !o.claimCurrent(claim) {
				continue
			}
			claim.err = o.app.claim()
			if ctx.Err() != nil {
				return
			}
			if claim.err != nil {
				claim.err = fmt.Errorf("claim active source: %w", claim.err)
				o.report(claim.err)
			}
			select {
			case o.claimResults <- claim:
			case <-ctx.Done():
				return
			}
		}
	}
}
