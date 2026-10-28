package cec

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type serviceBackend struct {
	busy       atomic.Int32
	concurrent atomic.Bool
	claims     atomic.Int32
	closed     atomic.Int32
	packets    chan ReceivedMessage
	events     chan AdapterEvent
	pollErr    error
}

func (b *serviceBackend) enter() func() {
	if b.busy.Add(1) != 1 {
		b.concurrent.Store(true)
	}
	return func() { b.busy.Add(-1) }
}
func (b *serviceBackend) Open(string) error        { defer b.enter()(); return nil }
func (b *serviceBackend) Close() error             { defer b.enter()(); b.closed.Add(1); return nil }
func (b *serviceBackend) ClaimActiveSource() error { defer b.enter()(); b.claims.Add(1); return nil }
func (b *serviceBackend) SetPower(bool) error      { defer b.enter()(); return nil }
func (b *serviceBackend) SelfID() (int, error)     { defer b.enter()(); return 4, nil }
func (b *serviceBackend) AdapterInfo() (AdapterInfo, error) {
	defer b.enter()()
	physical := "1.0.0.0"
	return AdapterInfo{LogicalAddresses: []int{4}, PhysicalAddress: &physical}, nil
}
func (b *serviceBackend) Poll() (*ReceivedMessage, *AdapterEvent, error) {
	defer b.enter()()
	if b.pollErr != nil {
		return nil, nil, b.pollErr
	}
	select {
	case packet := <-b.packets:
		return &packet, nil, nil
	case event := <-b.events:
		return nil, &event, nil
	case <-time.After(time.Millisecond):
		return nil, nil, nil
	}
}

func TestCECServiceSerializesCommandsAndSeparatesDiagnostics(t *testing.T) {
	b := &serviceBackend{packets: make(chan ReceivedMessage, 1), events: make(chan AdapterEvent, 1)}
	service, err := NewService(b, "mock-only")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	finished := make(chan error, 1)
	go func() { finished <- service.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-service.Ready():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	packet := ReceivedMessage{Raw: "4f822000", Length: 4}
	b.packets <- packet
	b.events <- AdapterEvent{Event: 1}
	var workers sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); failures <- service.ClaimActiveSource(ctx) }()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	select {
	case observation := <-service.Observations():
		if observation.Message.Raw != packet.Raw || observation.Message.Length != 4 || observation.Adapter.LogicalAddresses[0] != 4 {
			t.Fatal(observation)
		}
	case <-ctx.Done():
		t.Fatal("no observation")
	}
	select {
	case event := <-service.Diagnostics():
		if event.Event != 1 {
			t.Fatal(event)
		}
	case <-ctx.Done():
		t.Fatal("no diagnostic")
	}
	if b.concurrent.Load() || b.claims.Load() != 20 {
		t.Fatal("backend not serialized")
	}
}

func TestCECServiceFailureUnblocksCallAndClosesBackend(t *testing.T) {
	failure := errors.New("adapter lost")
	b := &serviceBackend{pollErr: failure}
	service, err := NewService(b, "mock-only")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Run(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := service.ClaimActiveSource(context.Background()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if b.closed.Load() != 1 {
		t.Fatal("backend not closed")
	}
}

type failedMetadataBackend struct {
	serviceBackend
	failure error
}

func (b *failedMetadataBackend) AdapterInfo() (AdapterInfo, error) {
	return AdapterInfo{}, b.failure
}

func TestCECServiceDoesNotForwardPacketWithoutCurrentMetadata(t *testing.T) {
	failure := errors.New("adapter metadata unavailable")
	b := &failedMetadataBackend{
		serviceBackend: serviceBackend{packets: make(chan ReceivedMessage, 1)},
		failure:        failure,
	}
	b.packets <- ReceivedMessage{Raw: "4f822000", Length: 4}
	service, err := NewService(b, "mock-only")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Run(ctx); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	if observation, ok := <-service.Observations(); ok {
		t.Fatalf("packet forwarded without current metadata: %+v", observation)
	}
	if b.closed.Load() != 1 {
		t.Fatal("backend not closed")
	}
}

func TestCECServiceRejectsInvalidConstruction(t *testing.T) {
	if _, err := NewService(nil, ""); err == nil {
		t.Fatal("invalid service accepted")
	}
}
