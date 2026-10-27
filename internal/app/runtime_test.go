package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeRejectsIncompleteConfiguration(t *testing.T) {
	if err := RunRuntime(context.Background(), RuntimeConfig{}, nil); err == nil {
		t.Fatal("incomplete config accepted")
	}
}

func TestRuntimeBadStateDoesNotBuildCatalog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	config := RuntimeConfig{Roots: []string{t.TempDir()}, StateDir: dir, TempDir: t.TempDir(), ClaimSource: func() error { return nil }, Show: func(string) {}, Report: func(error) {}}
	if err := RunRuntime(context.Background(), config, nil); err == nil {
		t.Fatal("invalid state accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog.json")); !os.IsNotExist(err) {
		t.Fatal("catalog touched after state load failure")
	}
}

func TestCancelledRuntimeDoesNotCreateState(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "not-created")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	config := RuntimeConfig{Roots: []string{t.TempDir()}, StateDir: stateDir, TempDir: t.TempDir(), ClaimSource: func() error { return nil }, Show: func(string) {}, Report: func(error) {}}
	if err := RunRuntime(ctx, config, nil); err == nil {
		t.Fatal("cancelled runtime accepted")
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("cancelled startup created files")
	}
}
