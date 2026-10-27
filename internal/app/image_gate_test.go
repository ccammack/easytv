package app

import (
	"context"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"testing"
	"time"
)

func TestImageGateRequiresElapsedAndPrepared(t *testing.T) {
	for _, elapsedFirst := range []bool{false, true} {
		var g imageGate
		release := make(chan struct{})
		g.reset(context.Background(), 3, "/photo.jpg", "/next.jpg", func(ctx context.Context, path string) (preparedmedia.Result, error) {
			select {
			case <-release:
				return preparedmedia.Result{OriginalPath: path, PlayPath: path}, nil
			case <-ctx.Done():
				return preparedmedia.Result{}, ctx.Err()
			}
		})
		if elapsedFirst {
			g.expire()
			if g.canAdvance() {
				t.Fatal("advanced without preparation")
			}
		}
		close(release)
		select {
		case result := <-g.ready:
			g.accept(result)
		case <-time.After(time.Second):
			t.Fatal("preparation blocked")
		}
		if !elapsedFirst {
			if g.canAdvance() {
				t.Fatal("advanced before display elapsed")
			}
			g.expire()
		}
		if !g.canAdvance() {
			t.Fatal("both gates did not permit advance")
		}
		g.clear()
	}
}

func TestImageGateCancellationJoinsWorkerAndRejectsStaleResult(t *testing.T) {
	var g imageGate
	g.reset(context.Background(), 1, "/photo.jpg", "/next.jpg", func(ctx context.Context, path string) (preparedmedia.Result, error) {
		<-ctx.Done()
		return preparedmedia.Result{}, ctx.Err()
	})
	g.clear()
	if g.timerC != nil || g.ready != nil || g.done != nil {
		t.Fatal("retired transition retained work")
	}
	g.generation = 2
	g.accept(preparedNext{generation: 1})
	if g.result != nil {
		t.Fatal("stale preparation accepted")
	}
}
