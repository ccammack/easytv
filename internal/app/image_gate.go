package app

import (
	"context"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"time"
)

const imageDisplayDuration = 10 * time.Second

type preparedNext struct {
	generation uint64
	media      preparedmedia.Result
	err        error
}

type imageGate struct {
	generation uint64
	image      bool
	elapsed    bool
	timer      *time.Timer
	timerC     <-chan time.Time
	ready      <-chan preparedNext
	result     *preparedNext
	cancel     context.CancelFunc
	done       <-chan struct{}
}

func (g *imageGate) reset(ctx context.Context, generation uint64, current, next string, prepare func(context.Context, string) (preparedmedia.Result, error)) {
	g.clear()
	g.generation = generation
	g.image = preparedmedia.IsImage(current)
	if g.image {
		g.timer = time.NewTimer(imageDisplayDuration)
		g.timerC = g.timer.C
	}
	workerCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	ready := make(chan preparedNext, 1)
	g.ready = ready
	done := make(chan struct{})
	g.done = done
	go func() {
		defer close(done)
		media, err := prepare(workerCtx, next)
		ready <- preparedNext{generation, media, err}
	}()
}

func (g *imageGate) accept(result preparedNext) {
	if result.generation != g.generation {
		return
	}
	g.result = &result
	g.ready = nil
}
func (g *imageGate) expire()          { g.elapsed = true; g.timerC = nil }
func (g *imageGate) canAdvance() bool { return g.image && g.elapsed && g.result != nil }
func (g *imageGate) clear() {
	if g.timer != nil {
		g.timer.Stop()
	}
	if g.cancel != nil {
		g.cancel()
		<-g.done
	}
	*g = imageGate{}
}
