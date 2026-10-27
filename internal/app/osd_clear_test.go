package app

import (
	"errors"
	"testing"
	"time"
)

func TestStopAndSourceAwayClearPreviewOverlay(t *testing.T) {
	for _, scenario := range []string{"stop", "source away", "standby", "save failure"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			ticket, err := f.app.RequestChannel(1, time.Unix(100, 0))
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("disk failed")
			switch scenario {
			case "source away":
				_, err = f.app.HandleCEC(frame("4f822000"), adapter())
			case "standby":
				_, err = f.app.HandleCEC(frame("089001"), adapter())
			case "save failure":
				f.saveErr = failure
				err = f.app.Stop()
			default:
				err = f.app.Stop()
			}
			if scenario == "save failure" {
				if !errors.Is(err, failure) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(f.shown) != 2 || f.shown[0] != "b" || f.shown[1] != "" || f.controller.Running() {
				t.Fatal("stop did not clear overlay")
			}
			if err := f.app.CommitPreview(ticket.Sequence, ticket.Due); err != nil {
				t.Fatal(err)
			}
			if len(f.shown) != 2 || f.player.starts != 1 {
				t.Fatal("cancelled ticket restored overlay or playback")
			}
		})
	}
}
