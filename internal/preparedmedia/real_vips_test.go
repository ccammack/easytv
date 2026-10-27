package preparedmedia

import (
	"context"
	"crypto/sha256"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealVipsPreparation(t *testing.T) {
	if os.Getenv("EASYTV_TEST_REAL_VIPS") != "1" {
		t.Skip("opt in with EASYTV_TEST_REAL_VIPS=1")
	}
	for _, size := range []image.Point{{X: 64, Y: 32}, {X: 2400, Y: 1200}} {
		t.Run(size.String(), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "original.PNG")
			f, err := os.Create(source)
			if err != nil {
				t.Fatal(err)
			}
			encodeErr := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, size.X, size.Y)))
			closeErr := f.Close()
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			original, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			p, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, err := p.Prepare(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			width, err := p.dimension(ctx, "width", result.PlayPath)
			if err != nil {
				t.Fatal(err)
			}
			height, err := p.dimension(ctx, "height", result.PlayPath)
			if err != nil {
				t.Fatal(err)
			}
			if width > 1920 || height > 1920 || !result.Temporary || result.OriginalPath != source {
				t.Fatalf("result=%+v dimensions=%dx%d", result, width, height)
			}
			if size.X <= 1920 {
				copied, err := os.ReadFile(result.PlayPath)
				if err != nil || sha256.Sum256(copied) != sha256.Sum256(original) {
					t.Fatal("small image was not copied unchanged")
				}
				if result.Downscaled || width != size.X || height != size.Y {
					t.Fatal("small image altered")
				}
			} else if !result.Downscaled || width != 1920 || height != 960 || filepath.Ext(result.PlayPath) != ".jpg" {
				t.Fatal("large image not downscaled as reference")
			}
			after, err := os.ReadFile(source)
			if err != nil || sha256.Sum256(after) != sha256.Sum256(original) {
				t.Fatal("original image changed")
			}
			t.Logf("source=%dx%d prepared=%dx%d downscaled=%v original unchanged", size.X, size.Y, width, height, result.Downscaled)
		})
	}
}
