package app

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/cec"
)

type OwnerSubmitter interface {
	Submit(context.Context, OwnerCommand) (OwnerResult, error)
}

// ForwardCEC never calls the adapter or changes playback directly. In
// particular it cannot block the adapter service while the owner requests AS.
func ForwardCEC(ctx context.Context, observations <-chan cec.Observation, owner OwnerSubmitter, report func(error)) error {
	if observations == nil || owner == nil || report == nil {
		return errors.New("CEC observations, owner and reporter required")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case observation, ok := <-observations:
			if !ok {
				return nil
			}
			result, err := owner.Submit(ctx, OwnerCommand{Kind: "cec", Message: observation.Message, Adapter: observation.Adapter})
			if err != nil {
				return err
			}
			if result.Err != nil {
				report(result.Err)
			}
		}
	}
}
