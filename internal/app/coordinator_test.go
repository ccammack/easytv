package app

import (
	"errors"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
)

type player struct {
	starts, stops int
	path          string
	stopErr       error
}

func (p *player) Start(path string, _ int64, _ uint64) error { p.starts++; p.path = path; return nil }
func (p *player) Stop() error                                { p.stops++; return p.stopErr }

type fixture struct {
	app           *Coordinator
	owner         *Owner
	controller    *playback.Controller
	preview       *playback.Preview
	player        *player
	claims, saves int
	shown         []string
	claimErr      error
	saveErr       error
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{player: &player{}}
	channels := []catalog.Channel{{Path: "a", Media: []string{"a.mp4"}}, {Path: "b", Media: []string{"b.mp4"}}, {Path: "c", Media: []string{"c.mp4"}}}
	var err error
	f.controller, err = playback.New(channels, state.Default(), f.player, func(state.State) error { f.saves++; return f.saveErr })
	if err != nil {
		t.Fatal(err)
	}
	f.preview, err = playback.NewPreview([]string{"a", "b", "c"}, playback.DefaultPreviewDelay)
	if err != nil {
		t.Fatal(err)
	}
	f.app, err = New(f.controller, f.preview, func() error { f.claims++; return f.claimErr }, func(id string) { f.shown = append(f.shown, id) })
	if err != nil {
		t.Fatal(err)
	}
	if err := f.controller.Start(); err != nil {
		t.Fatal(err)
	}
	// Scenario assertions count saves after the separately verified startup save.
	if f.saves != 1 {
		t.Fatal("startup was not persisted")
	}
	f.saves = 0
	f.owner = inputOwner(t, f.app)
	return f
}

func (f *fixture) request(direction int, now time.Time) (playback.PreviewTicket, error) {
	if err := f.owner.queueChannel(direction, now); err != nil {
		return playback.PreviewTicket{}, err
	}
	claim := <-f.owner.claims
	claim.err = f.app.claim()
	f.owner.finishClaim(claim)
	return claim.ticket, claim.err
}

func TestRapidRequestsClaimInOrderButCommitOnce(t *testing.T) {
	f := setup(t)
	now := time.Unix(100, 0)
	first, err := f.request(1, now)
	if err != nil {
		t.Fatal(err)
	}

	second, err := f.request(1, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if f.claims != 2 || f.player.starts != 1 || len(f.shown) != 2 || f.shown[1] != "c" {
		t.Fatalf("fixture=%+v", f)
	}
	if err := f.app.CommitPreview(first.Sequence, second.Due); err != nil {
		t.Fatal(err)
	}
	if f.player.starts != 1 {
		t.Fatal("stale commit started player")
	}
	if err := f.app.CommitPreview(second.Sequence, second.Due); err != nil {
		t.Fatal(err)
	}
	if f.player.path != "c.mp4" || f.player.starts != 2 || f.player.stops != 1 || f.claims != 2 || f.saves != 2 {
		t.Fatalf("fixture=%+v player=%+v", f, f.player)
	}
}
func TestFailedSwitchReconcilesPreviewBase(t *testing.T) {
	f := setup(t)
	ticket, err := f.request(1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("player stop failed")
	f.player.stopErr = failure
	if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); !errors.Is(err, failure) {
		t.Fatalf("commit error=%v", err)
	}
	if f.controller.SelectedChannelID() != "a" {
		t.Fatal("failed stop changed actual channel")
	}
	next, err := f.request(1, time.Now())
	if err != nil || next.ChannelID != "b" {
		t.Fatalf("next preview=%+v error=%v; wanted b relative to actual a", next, err)
	}
}

func TestStopCancelsQueuedPreview(t *testing.T) {
	f := setup(t)
	ticket, err := f.request(1, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); err != nil {
		t.Fatal(err)
	}
	if f.controller.Running() || f.player.starts != 1 || f.claims != 1 {
		t.Fatal("cancelled preview restarted or reclaimed source")
	}
	if err := f.app.Stop(); err != nil {
		t.Fatal(err)
	}
	if f.saves != 1 || f.player.stops != 1 {
		t.Fatal("repeat stop performed work")
	}
}
func TestStopCancelsEvenWhenSaveFails(t *testing.T) {
	f := setup(t)
	ticket, err := f.request(1, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("disk failed")
	f.saveErr = failure
	if err := f.app.Stop(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); err != nil {
		t.Fatal(err)
	}
	if f.player.starts != 1 || f.controller.Running() {
		t.Fatal("failed save allowed restart")
	}
}
func TestInvalidDirectionDoesNotClaim(t *testing.T) {
	f := setup(t)
	if _, err := f.request(0, time.Unix(100, 0)); err == nil || f.claims != 0 {
		t.Fatal("invalid request performed CEC")
	}
}
func TestWrappedCurrentDoesNotRestartUntilStopped(t *testing.T) {
	f := setup(t)
	now := time.Unix(100, 0)
	var ticket playback.PreviewTicket
	for i := 0; i < 3; i++ {
		var err error
		ticket, err = f.request(1, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); err != nil {
		t.Fatal(err)
	}
	if f.player.starts != 1 || f.claims != 3 {
		t.Fatal("wrapped selection restarted running player")
	}
	if err := f.app.Stop(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		var err error
		ticket, err = f.request(1, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); err != nil {
		t.Fatal(err)
	}
	if f.player.starts != 2 || !f.controller.Running() {
		t.Fatal("wrapped selection did not start stopped player")
	}
}
