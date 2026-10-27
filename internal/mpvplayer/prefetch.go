package mpvplayer

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"path/filepath"
)

// PrepareMedia reads only immutable configuration, so a preparation worker
// may call it. The worker must return results to the owner, not mutate Player.
func (p *Player) PrepareMedia(ctx context.Context, path string) (preparedmedia.Result, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return preparedmedia.Result{}, err
	}
	if p.config.Prepare != nil {
		return p.config.Prepare(ctx, absolute)
	}
	return preparedmedia.Result{OriginalPath: absolute, PlayPath: absolute}, nil
}

// QueuePrepared is owner-only. The next Start consumes this one-shot result.
func (p *Player) QueuePrepared(result preparedmedia.Result) error {
	if result.OriginalPath == "" || result.PlayPath == "" {
		return errors.New("prepared paths required")
	}
	original, err := filepath.Abs(result.OriginalPath)
	if err != nil {
		return err
	}
	result.OriginalPath = original
	p.prefetched = &result
	return nil
}

func (p *Player) takePrepared(original string) (preparedmedia.Result, error) {
	prepared := p.prefetched
	p.prefetched = nil
	if prepared != nil && prepared.OriginalPath == original {
		return *prepared, nil
	}
	return p.PrepareMedia(context.Background(), original)
}
