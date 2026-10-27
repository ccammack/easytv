package app

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/cec"
	"testing"
)

type recordingOwner struct {
	commands []OwnerCommand
	err      error
}

func (o *recordingOwner) Submit(_ context.Context, command OwnerCommand) (OwnerResult, error) {
	o.commands = append(o.commands, command)
	return OwnerResult{Err: o.err}, nil
}
func TestForwardCECPreservesPacketsAndReportsOwnerErrors(t *testing.T) {
	observations := make(chan cec.Observation, 2)
	for _, raw := range []string{"4f822000", "089001"} {
		observations <- cec.Observation{Message: cec.ReceivedMessage{Raw: raw}, Adapter: cec.AdapterInfo{LogicalAddresses: []int{8}}}
	}
	close(observations)
	failure := errors.New("save failed")
	owner := &recordingOwner{err: failure}
	reported := 0
	if err := ForwardCEC(context.Background(), observations, owner, func(err error) {
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		reported++
	}); err != nil {
		t.Fatal(err)
	}
	if len(owner.commands) != 2 || owner.commands[0].Kind != "cec" || owner.commands[0].Message.Raw != "4f822000" || owner.commands[1].Message.Raw != "089001" || owner.commands[0].Adapter.LogicalAddresses[0] != 8 || reported != 2 {
		t.Fatal("CEC forwarding changed or lost data")
	}
}
