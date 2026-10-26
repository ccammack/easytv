package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Position matches the existing Elixir state.json representation.
type Position struct {
	FilePath      *string `json:"file_path"`
	OffsetSeconds int64   `json:"offset_seconds"`
}
type State struct {
	Version          int                 `json:"version"`
	CurrentChannelID *string             `json:"current_channel_id"`
	ChannelState     map[string]Position `json:"channel_state"`
}

func Default() State { return State{Version: 1, ChannelState: make(map[string]Position)} }
func (s State) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported state version %d", s.Version)
	}
	if s.ChannelState == nil {
		return errors.New("channel_state must be an object")
	}
	for id, p := range s.ChannelState {
		if p.OffsetSeconds < 0 {
			return fmt.Errorf("channel %q: negative offset", id)
		}
	}
	return nil
}

// Load returns a default only for a missing file, never for invalid contents.
func Load(path string) (State, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("open state: %w", err)
	}
	defer f.Close()
	var s State
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&s); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return State{}, errors.New("state contains trailing data")
	}
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	return s, nil
}

// Save writes a private temporary file beside the destination and renames it.
// The caller serializes saves; this package does not own application state.
func Save(path string, s State) (result error) {
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
