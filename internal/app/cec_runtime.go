package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/cec"
)

// RunCECRuntime supervises adapter ownership and playback as one lifecycle.
// The backend is injected: callers must explicitly supply real hardware.
// Diagnostic callbacks must return promptly and are separate from packets.
func RunCECRuntime(ctx context.Context, config RuntimeConfig, backend cec.Backend, device string, ready chan<- *Owner, diagnostic func(cec.AdapterEvent)) (result error) {
	if diagnostic == nil {
		return errors.New("CEC diagnostic callback required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	service, err := cec.NewService(backend, device)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	serviceDone := make(chan error, 1)
	go func() { serviceDone <- service.Run(runCtx) }()
	var runtimeDone chan error
	var forwardDone chan error
	var diagnosticDone chan struct{}
	clean := func(err error) error {
		if err == context.Canceled || err == context.DeadlineExceeded {
			return nil
		}
		return err
	}
	defer func() {
		cancel()
		if runtimeDone != nil {
			result = errors.Join(result, clean(<-runtimeDone))
		}
		if forwardDone != nil {
			result = errors.Join(result, clean(<-forwardDone))
		}
		if serviceDone != nil {
			result = errors.Join(result, clean(<-serviceDone))
		}
		if diagnosticDone != nil {
			<-diagnosticDone
		}
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-serviceDone:
		serviceDone = nil
		return err
	case <-service.Ready():
	}
	diagnosticDone = make(chan struct{})
	go func() {
		defer close(diagnosticDone)
		for event := range service.Diagnostics() {
			diagnostic(event)
		}
	}()
	config.ClaimSource = func() error { return service.ClaimActiveSource(runCtx) }
	ownerReady := make(chan *Owner, 1)
	runtimeDone = make(chan error, 1)
	go func() { runtimeDone <- RunRuntime(runCtx, config, ownerReady) }()
	var owner *Owner
	select {
	case <-ctx.Done():
		return nil
	case err := <-serviceDone:
		serviceDone = nil
		if err == nil && ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("CEC service ended during startup: %w", err)
	case err := <-runtimeDone:
		runtimeDone = nil
		return err
	case owner = <-ownerReady:
	}
	forwardDone = make(chan error, 1)
	go func() { forwardDone <- ForwardCEC(runCtx, service.Observations(), owner, config.Report) }()
	if ready != nil {
		select {
		case ready <- owner:
		case <-ctx.Done():
			return nil
		case err := <-serviceDone:
			serviceDone = nil
			if err == nil && ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("CEC service ended before readiness: %w", err)
		case err := <-runtimeDone:
			runtimeDone = nil
			return err
		}
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-serviceDone:
		serviceDone = nil
		if err == nil && ctx.Err() == nil {
			err = errors.New("CEC service ended unexpectedly")
		}
		return err
	case err := <-runtimeDone:
		runtimeDone = nil
		if err == nil && ctx.Err() == nil {
			err = errors.New("playback runtime ended unexpectedly")
		}
		return err
	case err := <-forwardDone:
		forwardDone = nil
		if err == nil && ctx.Err() == nil {
			err = errors.New("CEC observation forwarding ended unexpectedly")
		}
		return err
	}
}
