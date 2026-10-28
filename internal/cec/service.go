package cec

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
)

// Observation preserves the received packet separately from adapter metadata.
type Observation struct {
	Message ReceivedMessage
	Adapter AdapterInfo
}
type serviceRequest struct {
	operation string
	reply     chan error
}

// Service exclusively serializes Backend access. Backend calls and packet
// delivery never run in the playback owner goroutine. Queue overflow is an
// explicit service failure, not a silently dropped CEC observation.
type Service struct {
	backend      Backend
	device       string
	requests     chan serviceRequest
	observations chan Observation
	diagnostics  chan AdapterEvent
	ready        chan struct{}
	done         chan struct{}
	started      atomic.Bool
	err          error // written before done closes; read only after receiving done
}

func NewService(backend Backend, device string) (*Service, error) {
	if backend == nil || device == "" {
		return nil, errors.New("CEC backend and device path required")
	}
	return &Service{backend: backend, device: device, requests: make(chan serviceRequest), observations: make(chan Observation, 64), diagnostics: make(chan AdapterEvent, 64), ready: make(chan struct{}), done: make(chan struct{})}, nil
}
func (s *Service) Ready() <-chan struct{}           { return s.ready }
func (s *Service) Observations() <-chan Observation { return s.observations }
func (s *Service) Diagnostics() <-chan AdapterEvent { return s.diagnostics }
func copyAdapter(info AdapterInfo) AdapterInfo {
	info.LogicalAddresses = append([]int(nil), info.LogicalAddresses...)
	if info.PhysicalAddress != nil {
		value := *info.PhysicalAddress
		info.PhysicalAddress = &value
	}
	return info
}
func (s *Service) Run(ctx context.Context) (result error) {
	if !s.started.CompareAndSwap(false, true) {
		return errors.New("CEC service can run only once")
	}
	defer func() {
		result = errors.Join(result, s.backend.Close())
		s.err = result
		close(s.observations)
		close(s.diagnostics)
		close(s.done)
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.backend.Open(s.device); err != nil {
		return err
	}
	close(s.ready)
	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-s.requests:
			var err error
			switch request.operation {
			case "claim":
				err = s.backend.ClaimActiveSource()
			case "power-on":
				err = s.backend.SetPower(true)
			case "power-off":
				err = s.backend.SetPower(false)
			default:
				err = errors.New("unknown CEC service operation")
			}
			request.reply <- err
		default:
			message, diagnostic, err := s.backend.Poll()
			if err != nil {
				return fmt.Errorf("CEC poll: %w", err)
			}
			if diagnostic != nil {
				select {
				case s.diagnostics <- *diagnostic:
				default:
					return errors.New("CEC diagnostic queue overflow")
				}
			}
			if message != nil {
				info, err := s.backend.AdapterInfo()
				if err != nil {
					return fmt.Errorf("CEC observation metadata: %w", err)
				}
				select {
				case s.observations <- Observation{Message: *message, Adapter: copyAdapter(info)}:
				default:
					return errors.New("CEC observation queue overflow")
				}
			}
		}
	}
}
func (s *Service) call(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	request := serviceRequest{operation: operation, reply: make(chan error, 1)}
	stopped := func() error {
		if s.err != nil {
			return s.err
		}
		return errors.New("CEC service stopped")
	}
	select {
	case s.requests <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return stopped()
	}
	select {
	case err := <-request.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return stopped()
	}
}
func (s *Service) ClaimActiveSource(ctx context.Context) error {
	return s.call(ctx, "claim")
}
func (s *Service) SetPower(ctx context.Context, on bool) error {
	operation := "power-off"
	if on {
		operation = "power-on"
	}
	return s.call(ctx, operation)
}
