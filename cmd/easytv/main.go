package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/ccammack/easytv/internal/app"
	"github.com/ccammack/easytv/internal/cec"
	"github.com/ccammack/easytv/internal/mpvplayer"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// CEC access requires an explicit device path; headless mode uses null outputs.
// Storage paths remain explicit to keep deployment separate from validation.
func runWithBackendFactory(ctx context.Context, args []string, stdout, stderr io.Writer, factory func(string) cec.Backend) error {
	flags := flag.NewFlagSet("easytv", flag.ContinueOnError)
	flags.SetOutput(stderr)
	headless := flags.Bool("headless", false, "use null audio/video outputs; CEC remains disabled unless explicitly selected")
	device := flags.String("cec-device", "", "explicit CEC device path; empty disables CEC in headless mode")
	listen := flags.String("listen", "", "optional HTTP bind address, e.g. 127.0.0.1:8080; disabled when empty")
	deviceName := flags.String("device-name", "rpi-tv", "device_name in the legacy HTTP protocol")
	scan := flags.Bool("scan", false, "rebuild the catalog for supplied media roots")
	stateDir := flags.String("state-dir", "", "explicit isolated state/catalog directory (required)")
	tempDir := flags.String("temp-dir", "", "explicit temporary workspace parent (required)")
	runFor := flags.Duration("run-for", 0, "optional bounded runtime, such as 30s")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: easytv [--headless] [--cec-device DEVICE] --state-dir DIR --temp-dir DIR [--scan] [--run-for DURATION] ROOT [ROOT ...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if !*headless && *device == "" {
		return errors.New("real output requires an explicit --cec-device; use --headless for isolated playback")
	}
	if *device != "" && factory == nil {
		return errors.New("CEC backend factory required")
	}
	if *stateDir == "" || *tempDir == "" || flags.NArg() == 0 {
		return errors.New("explicit --state-dir, --temp-dir and at least one media root required")
	}
	if *runFor < 0 {
		return errors.New("--run-for must not be negative")
	}
	var listener net.Listener
	if *listen != "" {
		var err error
		listener, err = net.Listen("tcp", *listen)
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		defer listener.Close()
	}
	runtimeCtx, cancel := context.WithCancel(ctx)
	if *runFor > 0 {
		cancel()
		runtimeCtx, cancel = context.WithTimeout(ctx, *runFor)
	}
	defer cancel()
	config := app.RuntimeConfig{HTTPListener: listener, DeviceName: *deviceName, Roots: flags.Args(), StateDir: *stateDir, TempDir: *tempDir, Scan: *scan, Player: mpvplayer.Config{OutputArgs: playerOutputArgs(*headless)}, ClaimSource: func() error { return nil }, Show: func(channel string) { fmt.Fprintf(stderr, "headless channel preview: %s\n", channel) }, Report: func(err error) { fmt.Fprintf(stderr, "easytv: %v\n", err) }}
	ready := make(chan *app.Owner, 1)
	finished := make(chan error, 1)
	go func() {
		if *device != "" {
			finished <- app.RunCECRuntime(runtimeCtx, config, factory(*device), *device, ready, func(event cec.AdapterEvent) { fmt.Fprintf(stderr, "CEC adapter diagnostic: %+v\n", event) })
		} else {
			finished <- app.RunRuntime(runtimeCtx, config, ready)
		}
	}()
	mode := "hardware"
	if *headless {
		mode = "headless"
	}
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
		fmt.Fprintf(stdout, "EasyTV %s runtime ready; running=%v\n", mode, status.Running)
		if listener != nil {
			fmt.Fprintf(stdout, "HTTP listening=%s (no built-in authentication or TLS)\n", listener.Addr())
		}
	case err := <-finished:
		return finish(err)
	case <-runtimeCtx.Done():
		return finish(<-finished)
	}
	err := finish(<-finished)
	if err == nil {
		fmt.Fprintf(stdout, "EasyTV %s runtime stopped\n", mode)
	}
	return err
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runWithBackendFactory(ctx, args, stdout, stderr, func(device string) cec.Backend { return cec.NewLinuxCECBackend(device) })
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "easytv:", err)
		os.Exit(1)
	}
}
