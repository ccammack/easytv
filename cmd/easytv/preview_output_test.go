package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/cec"
	"reflect"
	"strings"
	"testing"
)

type fakePreviewOverlay struct {
	shown          []string
	clears, closes int
	failure        error
}

func (o *fakePreviewOverlay) Show(text string) error {
	o.shown = append(o.shown, text)
	return o.failure
}
func (o *fakePreviewOverlay) Clear() error { o.clears++; return o.failure }
func (o *fakePreviewOverlay) Close() error { o.closes++; return o.failure }

func TestPreviewOutputIsOptInAndHeadlessSafe(t *testing.T) {
	for _, headless := range []bool{false, true} {
		var diagnostics bytes.Buffer
		called := false
		show, cleanup, err := newPreviewOutput(headless, false, &diagnostics, func() (previewOverlay, error) { called = true; return nil, errors.New("must not construct") })
		if err != nil {
			t.Fatal(err)
		}
		show("/media/Movies")
		show("")
		if err := cleanup(); err != nil {
			t.Fatal(err)
		}
		if called || !strings.Contains(diagnostics.String(), "channel preview: Movies") || strings.Contains(diagnostics.String(), "/media/") {
			t.Fatal("disabled OSD constructed an overlay or leaked path")
		}
	}
	called := false
	if _, _, err := newPreviewOutput(true, true, &bytes.Buffer{}, func() (previewOverlay, error) { called = true; return nil, nil }); err == nil || called {
		t.Fatal("headless OSD was not rejected before construction")
	}
}

func TestPreviewOutputDisplaysNamesClearsAndCloses(t *testing.T) {
	overlay := &fakePreviewOverlay{}
	var diagnostics bytes.Buffer
	show, cleanup, err := newPreviewOutput(false, true, &diagnostics, func() (previewOverlay, error) { return overlay, nil })
	if err != nil {
		t.Fatal(err)
	}
	show("/mnt/media/Movies (1940s)")
	show("/mnt/media/odd\nname")
	show("")
	if !reflect.DeepEqual(overlay.shown, []string{"Movies (1940s)", "odd name"}) || overlay.clears != 1 {
		t.Fatalf("overlay=%+v", overlay)
	}
	if err := cleanup(); err != nil || overlay.closes != 1 {
		t.Fatal("overlay cleanup missing")
	}
}

func TestPreviewOutputReportsFailuresWithoutPanicking(t *testing.T) {
	failure := errors.New("OSD unavailable")
	overlay := &fakePreviewOverlay{failure: failure}
	var diagnostics bytes.Buffer
	show, cleanup, err := newPreviewOutput(false, true, &diagnostics, func() (previewOverlay, error) { return overlay, nil })
	if err != nil {
		t.Fatal(err)
	}
	show("/media/Movies")
	show("")
	if strings.Count(diagnostics.String(), failure.Error()) != 2 {
		t.Fatal("show/clear errors not reported")
	}
	if err := cleanup(); !errors.Is(err, failure) {
		t.Fatal("close failure lost")
	}
	for _, factory := range []func() (previewOverlay, error){nil, func() (previewOverlay, error) { return nil, failure }, func() (previewOverlay, error) { return nil, nil }} {
		if _, _, err := newPreviewOutput(false, true, &diagnostics, factory); err == nil {
			t.Fatal("invalid overlay factory accepted")
		}
	}
}

func TestCLIRejectsHeadlessOSDBeforeBackendOrStorage(t *testing.T) {
	var output bytes.Buffer
	called := false
	factory := func(string) cec.Backend { called = true; return nil }
	err := runWithBackendFactory(context.Background(), []string{"--headless", "--osd", "--cec-device", "mock-only", "--state-dir", t.TempDir(), "--temp-dir", t.TempDir(), t.TempDir()}, &output, &output, factory)
	if err == nil || !strings.Contains(err.Error(), "--osd") || called {
		t.Fatalf("err=%v backend=%v", err, called)
	}
}
