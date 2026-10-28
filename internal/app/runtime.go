package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/catalog"
	"github.com/ccammack/easytv/internal/httpapi"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"github.com/ccammack/easytv/internal/playback"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"github.com/ccammack/easytv/internal/state"
	"net"
	"os"
	"path/filepath"
)

// RuntimeConfig requires explicit storage paths and injected side effects.
// Hardware opening and executable defaults belong to the eventual entrypoint.
// Show and Report must support concurrent calls from input and playback work.
type RuntimeConfig struct {
	HTTPListener net.Listener
	httpServer   *httpapi.Server
	Roots        []string
	StateDir     string
	TempDir      string
	Scan         bool
	Player       mpvplayer.Config
	ClaimSource  playback.ClaimSource
	Show         func(string)
	Report       func(error)
}

// RunRuntime assembles and runs the owner, then joins preparation work and
// removes only its own temporary directory. The optional ready channel receives
// the owner so external producers can Submit commands; it is not closed here.
func RunRuntime(ctx context.Context, config RuntimeConfig, ready chan<- *Owner) (result error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if config.HTTPListener != nil {
		defer config.HTTPListener.Close()
	}
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
	preview, err := playback.NewPreview(controller.ChannelIDs(), playback.DefaultPreviewDelay)
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
	owner.scan = func(scanCtx context.Context) ([]catalog.Channel, error) {
		replacement, err := catalog.Scan(scanCtx, config.Roots)
		if err != nil {
			return nil, err
		}
		channels, err := catalog.ActiveChannels(replacement, config.Roots)
		if err != nil {
			return nil, err
		}
		if len(channels) == 0 {
			return nil, errors.New("rescan found no playable channels; existing catalog retained")
		}
		if err := scanCtx.Err(); err != nil {
			return nil, err
		}
		if err := catalog.SaveCatalog(filepath.Join(stateDir, "catalog.json"), replacement); err != nil {
			return nil, fmt.Errorf("save rescanned catalog: %w", err)
		}
		return channels, nil
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
	if config.HTTPListener == nil {
		return owner.Run(ctx)
	}
	server := config.httpServer
	if server == nil {
		server = httpapi.NewServer(nil)
	}
	return serveOwner(ctx, cancel, owner, server, config.HTTPListener)
}
