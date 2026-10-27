package mpvipc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

// EasyTV deliberately restricts software volume to 0..100 (no amplification).
func validateVolume(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		return fmt.Errorf("player volume must be a finite number from 0 to 100")
	}
	return nil
}
func (c *Client) SetVolume(ctx context.Context, value float64) error {
	if err := validateVolume(value); err != nil {
		return err
	}
	_, err := c.Call(ctx, "set_property", "volume", value)
	return err
}
func (c *Client) SetMute(ctx context.Context, muted bool) error {
	_, err := c.Call(ctx, "set_property", "mute", muted)
	return err
}
func (c *Client) Volume(ctx context.Context) (float64, error) {
	raw, err := c.Call(ctx, "get_property", "volume")
	if err != nil {
		return 0, err
	}
	var value *float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, fmt.Errorf("decode player volume: %w", err)
	}
	if value == nil {
		return 0, fmt.Errorf("player volume unavailable")
	}
	if err := validateVolume(*value); err != nil {
		return 0, err
	}
	return *value, nil
}
func (c *Client) Muted(ctx context.Context) (bool, error) {
	raw, err := c.Call(ctx, "get_property", "mute")
	if err != nil {
		return false, err
	}
	var value *bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fmt.Errorf("decode player mute: %w", err)
	}
	if value == nil {
		return false, fmt.Errorf("player mute unavailable")
	}
	return *value, nil
}
