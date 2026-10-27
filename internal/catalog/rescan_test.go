package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveScanCompleteReplacementAndCancellation(t *testing.T) {
	root := t.TempDir()
	channel := filepath.Join(root, "channel")
	if err := os.Mkdir(channel, 0700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(channel, "first.mp4")
	if err := os.WriteFile(first, nil, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := Scan(context.Background(), []string{root, root})
	if err != nil || len(before.Roots) != 1 {
		t.Fatalf("scan=%+v err=%v", before, err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(channel, "second.mp4")
	if err := os.WriteFile(second, nil, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := Scan(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	active, err := ActiveChannels(after, []string{root})
	if err != nil || len(active) != 1 || len(active[0].Media) != 1 || active[0].Media[0] != second {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	if _, err := Scan(context.Background(), []string{root, filepath.Join(root, "missing")}); err == nil {
		t.Fatal("partial scan accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, []string{root}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan: %v", err)
	}
	if _, err := Scan(context.Background(), nil); err == nil {
		t.Fatal("empty roots accepted")
	}
}
