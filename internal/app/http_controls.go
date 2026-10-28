package app

import (
	"context"

	"github.com/ccammack/easytv/internal/httpapi"
)

type ownerHTTPControls struct{ owner *Owner }

func (c ownerHTTPControls) Status(ctx context.Context) (httpapi.PlaybackStatus, error) {
	result, err := c.owner.Submit(ctx, OwnerCommand{Kind: "status"})
	if err != nil {
		return httpapi.PlaybackStatus{}, err
	}
	return result.Status, result.Err
}

func (c ownerHTTPControls) submit(ctx context.Context, command OwnerCommand) error {
	result, err := c.owner.Submit(ctx, command)
	if err != nil {
		return err
	}
	return result.Err
}
func (c ownerHTTPControls) ChangeChannel(ctx context.Context, direction int) error {
	return c.submit(ctx, OwnerCommand{Kind: "channel", Direction: direction})
}
func (c ownerHTTPControls) SetVolume(ctx context.Context, value float64) error {
	return c.submit(ctx, OwnerCommand{Kind: "volume", Volume: value})
}
func (c ownerHTTPControls) SetMute(ctx context.Context, value bool) error {
	return c.submit(ctx, OwnerCommand{Kind: "mute", Muted: value})
}
func (c ownerHTTPControls) AdjustVolume(ctx context.Context, delta float64) error {
	return c.submit(ctx, OwnerCommand{Kind: "volume-adjust", Volume: delta})
}
func (c ownerHTTPControls) ToggleMute(ctx context.Context) error {
	return c.submit(ctx, OwnerCommand{Kind: "mute-toggle"})
}
func (c ownerHTTPControls) Rescan(ctx context.Context) error {
	return c.submit(ctx, OwnerCommand{Kind: "rescan"})
}
