package preparedmedia

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReferenceImageExtensions(t *testing.T) {
	for _, path := range []string{"a.JPG", "a.jpeg", "a.jpe", "a.jfif", "a.jif", "a.png", "a.gif", "a.bmp", "a.tif", "a.tiff", "a.webp", "a.ico", "a.pbm", "a.pgm", "a.ppm", "a.pnm", "a.tga"} {
		if !IsImage(path) {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"a.mp4", "a.wav", "a.svg", "a.jpg.mp4", ""} {
		if IsImage(path) {
			t.Fatal(path)
		}
	}
}

func TestNonImagePassesThroughWithoutOpeningMedia(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.run = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("nonimage invoked tool")
		return nil, nil
	}
	path := filepath.Join(t.TempDir(), "not-mounted.mp4")
	result, err := p.Prepare(context.Background(), path)
	if err != nil || result.OriginalPath != path || result.PlayPath != path || result.Temporary || result.Downscaled {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestSmallImagesCopiedAndSweepKeepsFour(t *testing.T) {
	dir := t.TempDir()
	p, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "vipsheader" || len(args) != 3 || args[0] != "-f" {
			t.Fatalf("tool=%s args=%v", name, args)
		}
		return []byte("64\n"), nil
	}
	source := filepath.Join(t.TempDir(), "original.PNG")
	original := []byte("mock image bytes")
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "unrelated.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		result, err := p.Prepare(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Temporary || result.Downscaled || result.PlayPath == source || result.OriginalPath != source || filepath.Ext(result.PlayPath) != ".png" {
			t.Fatalf("result=%+v", result)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != KeepLastFiles+1 {
		t.Fatalf("entries=%d want=%d", len(entries), KeepLastFiles+1)
	}
	data, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("original changed")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("sweep touched unrelated file")
	}
	if _, err := p.Prepare(context.Background(), source); err == nil {
		t.Fatal("closed preparer accepted work")
	}
}

func TestPreparationErrors(t *testing.T) {
	for _, scenario := range []string{"missing", "header failure", "bad dimensions", "thumbnail failure", "missing output", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			p, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			source := filepath.Join(t.TempDir(), "image.png")
			if scenario != "missing" {
				if err := os.WriteFile(source, []byte("mock"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p.run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
				if scenario == "header failure" || name == "vipsthumbnail" && scenario == "thumbnail failure" {
					return nil, errors.New("tool failed")
				}
				if scenario == "bad dimensions" {
					return []byte("not an integer"), nil
				}
				if name == "vipsheader" {
					return []byte("2400"), nil
				}
				return nil, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			if _, err := p.Prepare(ctx, source); err == nil {
				t.Fatal("failure accepted")
			}
		})
	}
}
