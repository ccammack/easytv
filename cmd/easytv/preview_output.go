package main

import (
	"errors"
	"fmt"
	"github.com/ccammack/easytv/internal/osd"
	"io"
	"path/filepath"
	"strings"
)

type previewOverlay interface {
	Show(string) error
	Clear() error
	Close() error
}

func previewOutput(headless, enabled bool, stderr io.Writer) (func(string), func() error, error) {
	return newPreviewOutput(headless, enabled, stderr, func() (previewOverlay, error) {
		return osd.New(osd.Config{Report: func(err error) { fmt.Fprintf(stderr, "easytv: %v\n", err) }})
	})
}

func newPreviewOutput(headless, enabled bool, stderr io.Writer, factory func() (previewOverlay, error)) (func(string), func() error, error) {
	if headless && enabled {
		return nil, nil, errors.New("--osd cannot be combined with --headless")
	}
	label := func(channel string) string {
		return strings.NewReplacer("\r", " ", "\n", " ").Replace(filepath.Base(channel))
	}
	if !enabled {
		return func(channel string) {
			if channel != "" {
				fmt.Fprintf(stderr, "channel preview: %s\n", label(channel))
			}
		}, func() error { return nil }, nil
	}
	if factory == nil {
		return nil, nil, errors.New("OSD factory required")
	}
	overlay, err := factory()
	if err != nil {
		return nil, nil, err
	}
	if overlay == nil {
		return nil, nil, errors.New("OSD factory returned nil")
	}
	show := func(channel string) {
		var err error
		if channel == "" {
			err = overlay.Clear()
		} else {
			err = overlay.Show(label(channel))
		}
		if err != nil {
			fmt.Fprintf(stderr, "easytv: OSD preview: %v\n", err)
		}
	}
	return show, overlay.Close, nil
}
