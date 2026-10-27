package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/ccammack/easytv/internal/app"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// This first entrypoint is deliberately headless-only and requires explicit
// storage paths. Real CEC/display defaults are not enabled by this checkpoint.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("easytv", flag.ContinueOnError)
	flags.SetOutput(stderr)
	headless := flags.Bool("headless", false, "use null audio/video outputs and no CEC hardware")
	scan := flags.Bool("scan", false, "rebuild the catalog for supplied media roots")
	stateDir := flags.String("state-dir", "", "explicit isolated state/catalog directory (required)")
	tempDir := flags.String("temp-dir", "", "explicit temporary workspace parent (required)")
	runFor := flags.Duration("run-for", 0, "optional bounded runtime, such as 30s")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: easytv --headless --state-dir DIR --temp-dir DIR [--scan] [--run-for DURATION] ROOT [ROOT ...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if !*headless {
		return errors.New("this checkpoint requires --headless; hardware playback is not enabled")
	}
	if *stateDir == "" || *tempDir == "" || flags.NArg() == 0 {
		return errors.New("explicit --state-dir, --temp-dir and at least one media root required")
	}
	if *runFor < 0 {
		return errors.New("--run-for must not be negative")
	}
	runtimeCtx, cancel := context.WithCancel(ctx)
	if *runFor > 0 {
		cancel()
		runtimeCtx, cancel = context.WithTimeout(ctx, *runFor)
	}
	defer cancel()
	config := app.RuntimeConfig{Roots: flags.Args(), StateDir: *stateDir, TempDir: *tempDir, Scan: *scan, Player: mpvplayer.Config{OutputArgs: []string{"--ao=null", "--vo=null"}}, ClaimSource: func() error { return nil }, Show: func(channel string) { fmt.Fprintf(stderr, "headless channel preview: %s\n", channel) }, Report: func(err error) { fmt.Fprintf(stderr, "easytv: %v\n", err) }}
	ready := make(chan *app.Owner, 1)
	finished := make(chan error, 1)
	go func() { finished <- app.RunRuntime(runtimeCtx, config, ready) }()
	finish := func(err error) error {
		if runtimeCtx.Err() != nil && err == runtimeCtx.Err() {
			return nil
		}
		return err
	}
	select {
	case owner := <-ready:
		status, err := owner.Submit(runtimeCtx, app.OwnerCommand{Kind: "status"})
		if err != nil || status.Err != nil {
			cancel()
			return errors.Join(err, status.Err, finish(<-finished))
		}
		fmt.Fprintf(stdout, "EasyTV headless runtime ready; running=%v\n", status.Running)
	case err := <-finished:
		return finish(err)
	case <-runtimeCtx.Done():
		return finish(<-finished)
	}
	err := finish(<-finished)
	if err == nil {
		fmt.Fprintln(stdout, "EasyTV headless runtime stopped")
	}
	return err
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "easytv:", err)
		os.Exit(1)
	}
}
