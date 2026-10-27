package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"github.com/ccammack/easytv/internal/state"
	"os"
	"path/filepath"
)

// RuntimeConfig requires explicit storage paths and injected side effects.
// Hardware opening and executable defaults belong to the eventual entrypoint.
type RuntimeConfig struct {
	Roots       []string
	StateDir    string
	TempDir     string
	Scan        bool
	Player      mpvplayer.Config
	ClaimSource playback.ClaimSource
	Show        func(string)
	Report      func(error)
}

// RunRuntime assembles and runs the owner, then joins preparation work and
// removes only its own temporary directory. The optional ready channel receives
// the owner so external producers can Submit commands; it is not closed here.
func RunRuntime(ctx context.Context, config RuntimeConfig, ready chan<- *Owner) (result error) {
	if len(config.Roots) == 0 || config.StateDir == "" || config.TempDir == "" {
		return errors.New("media roots and explicit state/temp directories required")
	}
	if config.ClaimSource == nil || config.Show == nil || config.Report == nil {
		return errors.New("AS, OSD and error callbacks required")
	}
	if config.Player.Prepare != nil {
		return errors.New("runtime owns media preparation; Player.Prepare must be unset")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stateDir, err := filepath.Abs(config.StateDir)
	if err != nil {
		return err
	}
	// Read state before creating/rebuilding the catalog, matching reference startup.
	saved, err := state.Load(filepath.Join(stateDir, "state.json"))
	if err != nil {
		return fmt.Errorf("load playback state: %w", err)
	}
	cached, err := catalog.LoadOrBuildCatalog(filepath.Join(stateDir, "catalog.json"), config.Roots, config.Scan, config.Report)
	if err != nil {
		return err
	}
	channels, err := catalog.ActiveChannels(cached, config.Roots)
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return errors.New("no playable media found in supplied roots")
	}
	for _, channel := range channels {
		if _, ok := saved.ChannelState[channel.Path]; !ok {
			saved.ChannelState[channel.Path] = state.Position{}
		}
	}
	if err := os.MkdirAll(config.TempDir, 0700); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp(config.TempDir, "easytv-prepared-")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, os.RemoveAll(workDir)) }()
	preparer, err := preparedmedia.New(workDir)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, preparer.Close()) }()
	playerConfig := config.Player
	// Tie synchronous preparation to runtime cancellation, including Start calls.
	playerConfig.Prepare = func(requestCtx context.Context, path string) (preparedmedia.Result, error) {
		prepareCtx, cancel := context.WithCancel(requestCtx)
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		defer cancel()
		if ctx.Err() != nil {
			cancel()
		}
		return preparer.Prepare(prepareCtx, path)
	}
	player := mpvplayer.New(playerConfig)
	controller, err := playback.New(channels, saved, player, func(s state.State) error { return state.Save(filepath.Join(stateDir, "state.json"), s) })
	if err != nil {
		return err
	}
	ids := make([]string, len(channels))
	for i, channel := range channels {
		ids[i] = channel.Path
	}
	preview, err := playback.NewPreview(ids, playback.DefaultPreviewDelay)
	if err != nil {
		return err
	}
	coordinator, err := New(controller, preview, config.ClaimSource, config.Show)
	if err != nil {
		return err
	}
	owner, err := NewOwner(controller, coordinator, player, config.Report)
	if err != nil {
		return err
	}
	if ready != nil {
		select {
		case ready <- owner:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return owner.Run(ctx)
}
