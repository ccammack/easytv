package app

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/httpapi"
	"net"
)

// serveOwner couples HTTP lifetime to playback. The supplied cancellation also
// interrupts preparation work owned by RunRuntime.
func serveOwner(ctx context.Context, cancel context.CancelFunc, owner *Owner, server *httpapi.Server, listener net.Listener) error {
	ownerDone, httpDone := make(chan error, 1), make(chan error, 1)
	go func() { ownerDone <- owner.Run(ctx) }()
	go func() { httpDone <- server.Serve(ctx, listener) }()
	var ownerErr, httpErr error
	ownerEnded, httpEnded := false, false
	select {
	case <-ctx.Done():
	case ownerErr = <-ownerDone:
		ownerEnded = true
	case httpErr = <-httpDone:
		httpEnded = true
	}
	cancel()
	if !ownerEnded {
		ownerErr = <-ownerDone
	}
	if !httpEnded {
		httpErr = <-httpDone
	}
	return errors.Join(ownerErr, httpErr)
}

type streamPublishingOwner struct {
	owner  *Owner
	server *httpapi.Server
}

func (s *streamPublishingOwner) Submit(ctx context.Context, command OwnerCommand) (OwnerResult, error) {
	if command.Kind == "cec" {
		s.server.PublishReceived(command.Message)
	}
	return s.owner.Submit(ctx, command)
}
