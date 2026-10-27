package main

import (
	"bytes"
	"context"
	"github.com/ccammack/easytv/internal/cec"
	"reflect"
	"testing"
)

func TestExplicitOutputModes(t *testing.T) {
	if got := playerOutputArgs(true); !reflect.DeepEqual(got, []string{"--ao=null", "--vo=null"}) {
		t.Fatal(got)
	}
	want := []string{"--force-window=yes", "--fullscreen", "--title=easytv", "--osd-level=0", "--cache=no", "--profile=fast", "--hwdec=auto"}
	if got := playerOutputArgs(false); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestHardwareInvocationRequiresExplicitCECDevice(t *testing.T) {
	var output bytes.Buffer
	called := false
	factory := func(string) cec.Backend { called = true; return nil }
	err := runWithBackendFactory(context.Background(), []string{"--state-dir", t.TempDir(), "--temp-dir", t.TempDir(), t.TempDir()}, &output, &output, factory)
	if err == nil || called {
		t.Fatal("hardware mode accepted without explicit device")
	}
}

func TestHeadlessWithoutCECDoesNotConstructBackend(t *testing.T) {
	var output bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	factory := func(string) cec.Backend { called = true; return nil }
	_ = runWithBackendFactory(ctx, []string{"--headless", "--state-dir", t.TempDir(), "--temp-dir", t.TempDir(), t.TempDir()}, &output, &output, factory)
	if called {
		t.Fatal("headless no-device mode constructed CEC backend")
	}
}
