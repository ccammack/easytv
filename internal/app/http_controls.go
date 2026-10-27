package app

import "context"

type ownerHTTPControls struct{ owner *Owner }

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
