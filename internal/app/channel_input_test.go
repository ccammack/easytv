package app

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/state"
)

func TestUnrelatedCECDoesNotCancelStoppedChannelPreview(t *testing.T) {
	o, ctx, _ := rescanOwner(t, false, nil)
	checkStoppedPreviewCEC(t, o, ctx, false)
}

func TestRealUnrelatedCECDoesNotCancelStandbyResume(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	o, ctx, _ := rescanOwner(t, true, nil)
	rescanSubmit(t, o, ctx, OwnerCommand{Kind: "status"})
	rescanSubmit(t, o, ctx, OwnerCommand{
		Kind: "cec", Message: cec.ReceivedMessage{Raw: "0f36", Length: 2},
		Adapter: cec.AdapterInfo{LogicalAddresses: []int{4}},
	})
	checkStoppedPreviewCEC(t, o, ctx, true)
}

func TestRealTVSelectsPiResumesAfterStandby(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_MPV") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_MPV=1")
	}
	o, ctx, _ := rescanOwner(t, true, nil)
	initial := rescanSubmit(t, o, ctx, OwnerCommand{Kind: "status"})
	if !initial.Running {
		t.Fatal("initial playback not running")
	}
	stopped := rescanSubmit(t, o, ctx, OwnerCommand{
		Kind: "cec", Message: frame("0f36"), Adapter: adapter(),
	})
	if stopped.Running {
		t.Fatal("standby did not stop playback")
	}
	resumed := rescanSubmit(t, o, ctx, OwnerCommand{
		Kind: "cec", Message: frame("0f861000"), Adapter: adapter(),
	})
	if !resumed.Running || resumed.Reason != "tv_selected_pi" ||
		resumed.Status.ChannelID != initial.Status.ChannelID ||
		resumed.Status.FilePath != initial.Status.FilePath {
		t.Fatalf("TV selection did not restore selected playback: %+v", resumed)
	}
	repeated := rescanSubmit(t, o, ctx, OwnerCommand{
		Kind: "cec", Message: frame("0f861000"), Adapter: adapter(),
	})
	if repeated.Generation != resumed.Generation {
		t.Fatal("duplicate selection restarted player")
	}
}

func checkStoppedPreviewCEC(t *testing.T, o *Owner, ctx context.Context, wantRunning bool) {
	t.Helper()
	rescanSubmit(t, o, ctx, OwnerCommand{Kind: "status"})
	quickChannel(t, o, 1)
	// Send unrelated bus traffic repeatedly across both claim completion and
	// the preview deadline, as occurs when a TV powers back on.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result := rescanSubmit(t, o, ctx, OwnerCommand{
			Kind: "cec", Message: cec.ReceivedMessage{Raw: "0f84000000", Length: 5},
			Adapter: cec.AdapterInfo{LogicalAddresses: []int{4}},
		})
		if result.Reason != "" {
			t.Fatalf("unrelated packet caused stop: %+v", result)
		}
		if result.Status.ChannelID == "/b" && result.Status.PendingChannelID == "" {
			if result.Running != wantRunning {
				t.Fatalf("committed playback running=%v want=%v", result.Running, wantRunning)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("unrelated CEC traffic prevented stopped channel preview from committing")
}

func waitChannelClaims(t *testing.T, claims *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for claims.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("claims=%d want=%d", claims.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func inputOwner(t *testing.T, a *Coordinator) *Owner {
	t.Helper()
	o, err := NewOwner(a.controller, a, &mpvplayer.Player{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	o.accepting = true
	close(o.inputReady)
	return o
}

func quickChannel(t *testing.T, o *Owner, direction int) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := o.Submit(ctx, OwnerCommand{Kind: "channel", Direction: direction})
	if err != nil || result.Err != nil {
		t.Fatalf("channel: result=%+v error=%v", result, err)
	}
	if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
		t.Fatalf("feedback took %s", elapsed)
	}
}

func TestChannelBurstFeedbackDoesNotWaitForCEC(t *testing.T) {
	f := setup(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.app.claim = func() error {
		close(entered)
		<-release
		return nil
	}
	o := inputOwner(t, f.app)
	ctx, cancel := context.WithCancel(context.Background())
	go o.runClaims(ctx)
	defer func() {
		cancel()
		close(release)
		<-o.claimsDone
	}()
	quickChannel(t, o, 1)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("claim not started")
	}
	for _, direction := range []int{1, -1, 1, 1, -1, -1, 1} {
		quickChannel(t, o, direction)
	}
	want := []string{"b", "c", "b", "c", "a", "c", "b", "c"}
	if len(f.shown) != len(want) {
		t.Fatalf("feedback=%v", f.shown)
	}
	for i := range want {
		if f.shown[i] != want[i] {
			t.Fatalf("feedback order=%v want=%v", f.shown, want)
		}
	}
	// Cancellation invalidates every queued claim, not just the final timer.
	f.app.cancelPreview()
}

type blockedInputPlayer struct {
	starts  int
	entered chan struct{}
	release chan struct{}
}

func (p *blockedInputPlayer) Start(string, int64, uint64) error {
	p.starts++
	if p.starts == 2 {
		close(p.entered)
		<-p.release
	}
	return nil
}
func (*blockedInputPlayer) Stop() error { return nil }

func TestChannelFeedbackDoesNotWaitForPlayerStartup(t *testing.T) {
	p := &blockedInputPlayer{entered: make(chan struct{}), release: make(chan struct{})}
	c, err := playback.New([]catalog.Channel{
		{Path: "a", Media: []string{"a.mp4"}},
		{Path: "b", Media: []string{"b.mp4"}},
		{Path: "c", Media: []string{"c.mp4"}},
	}, state.Default(), p, func(state.State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	preview, err := playback.NewPreview(c.ChannelIDs(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	shown := make(chan string, 8)
	a, err := New(c, preview, func() error { return nil }, func(text string) { shown <- text })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	o := inputOwner(t, a)
	quickChannel(t, o, 1)
	claim := <-o.claims
	done := make(chan error, 1)
	go func() { done <- a.CommitPreview(claim.ticket.Sequence, claim.ticket.Due) }()
	defer func() {
		close(p.release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("player did not block")
	}
	quickChannel(t, o, 1)
	if first, second := <-shown, <-shown; first != "b" || second != "c" {
		t.Fatalf("feedback=%q,%q", first, second)
	}
}

func TestChannelQueueRejectsOverflowWithoutChangingFeedback(t *testing.T) {
	f := setup(t)
	o := inputOwner(t, f.app)
	for range cap(o.claims) {
		quickChannel(t, o, 1)
	}

	before, _ := f.app.pending()
	count := len(f.shown)
	result, err := o.Submit(context.Background(), OwnerCommand{Kind: "channel", Direction: 1})
	after, _ := f.app.pending()
	if err != nil || result.Err == nil || before != after || len(f.shown) != count {
		t.Fatalf("overflow changed preview: error=%v result=%+v", err, result)
	}
}

func TestUnacceptedStopDoesNotCancelFeedback(t *testing.T) {
	for _, command := range []OwnerCommand{
		{Kind: "stop"},
		{Kind: "cec", Message: frame("0f36"), Adapter: adapter()},
	} {
		t.Run(command.Kind, func(t *testing.T) {
			f := setup(t)
			o := inputOwner(t, f.app)
			quickChannel(t, o, 1)
			before, _ := f.app.pending()
			epoch, shown := f.app.epoch, len(f.shown)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			// No owner receives the request: the context must expire before acceptance.
			if _, err := o.Submit(ctx, command); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("submit error=%v", err)
			}
			after, ok := f.app.pending()
			if !ok || after != before || f.app.epoch != epoch || len(f.shown) != shown {
				t.Fatal("unaccepted command changed pending feedback or invalidated claims")
			}
		})
	}
}

func TestStaleClaimFailureCannotClearNewFeedback(t *testing.T) {
	f := setup(t)
	o := inputOwner(t, f.app)
	quickChannel(t, o, 1)
	old := <-o.claims
	quickChannel(t, o, 1)
	old.err = errors.New("CEC failure")
	if _, ok := o.finishClaim(old); ok {
		t.Fatal("stale claim accepted")
	}
	pending, ok := f.app.pending()
	if !ok || pending.ChannelID != "c" {
		t.Fatal("stale failure cleared newer input")
	}
	latest := <-o.claims
	latest.err = old.err
	if _, ok := o.finishClaim(latest); !ok {
		t.Fatal("latest failure not handled")
	}
	if _, ok := f.app.pending(); ok || f.shown[len(f.shown)-1] != "" {
		t.Fatal("failed latest claim left pending preview")
	}
}
