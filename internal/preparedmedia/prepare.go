package preparedmedia

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const MaxImageDimension = 1920
const KeepLastFiles = 4

var imageExtensions = map[string]bool{}

func init() {
	for _, ext := range strings.Fields("jpg jpeg jpe jfif jif png gif bmp tif tiff webp ico pbm pgm ppm pnm tga") {
		imageExtensions["."+ext] = true
	}
}
func IsImage(path string) bool { return imageExtensions[strings.ToLower(filepath.Ext(path))] }

type Result struct {
	OriginalPath string
	PlayPath     string
	Temporary    bool
	Downscaled   bool
}

// Preparer owns a dedicated temporary image directory. Its sweep worker is
// joined by Close. Callers keep original paths in persistent playback state.
type Preparer struct {
	dir      string
	mu       sync.Mutex
	closed   bool
	sweepErr error
	sweep    chan struct{}
	stop     chan struct{}
	done     chan struct{}
	run      func(context.Context, string, ...string) ([]byte, error)
}

func New(dir string) (*Preparer, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	p := &Preparer{dir: absolute, sweep: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	p.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	go p.sweepLoop()
	return p, nil
}

func (p *Preparer) Close() error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.stop)
	}
	p.mu.Unlock()
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sweepErr
}

func (p *Preparer) Prepare(ctx context.Context, path string) (Result, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return Result{}, errors.New("media preparer closed")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !IsImage(absolute) {
		return Result{OriginalPath: absolute, PlayPath: absolute}, nil
	}
	defer func() {
		select {
		case p.sweep <- struct{}{}:
		default:
		}
	}()
	src, err := os.Open(absolute)
	if err != nil {
		return Result{}, fmt.Errorf("open image source: %w", err)
	}
	defer src.Close()
	copyFile, err := os.CreateTemp(p.dir, "src-*"+strings.ToLower(filepath.Ext(absolute)))
	if err != nil {
		return Result{}, err
	}
	copied := copyFile.Name()
	_, copyErr := io.Copy(copyFile, src)
	closeErr := copyFile.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return Result{}, fmt.Errorf("copy image: %w", err)
	}
	width, err := p.dimension(ctx, "width", copied)
	if err != nil {
		return Result{}, err
	}
	height, err := p.dimension(ctx, "height", copied)
	if err != nil {
		return Result{}, err
	}
	if width <= MaxImageDimension && height <= MaxImageDimension {
		return Result{OriginalPath: absolute, PlayPath: copied, Temporary: true}, nil
	}
	output, err := os.CreateTemp(p.dir, "out-*.jpg")
	if err != nil {
		return Result{}, err
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		return Result{}, err
	}
	// Remove the reservation so a successful tool must actually create output.
	if err := os.Remove(outputPath); err != nil {
		return Result{}, err
	}
	text, err := p.run(ctx, "vipsthumbnail", copied, "--size", "1920x1920", "--linear", "-o", outputPath+"[Q=90]")
	if err != nil {
		return Result{}, fmt.Errorf("vipsthumbnail: %w; output: %s", err, text)
	}
	info, err := os.Stat(outputPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return Result{}, fmt.Errorf("vipsthumbnail produced no regular nonempty output: %v", err)
	}
	return Result{OriginalPath: absolute, PlayPath: outputPath, Temporary: true, Downscaled: true}, nil
}

func (p *Preparer) dimension(ctx context.Context, field, path string) (int, error) {
	output, err := p.run(ctx, "vipsheader", "-f", field, path)
	if err != nil {
		return 0, fmt.Errorf("vipsheader %s: %w; output: %s", field, err, output)
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid vipsheader %s: %q", field, output)
	}
	return value, nil
}

func (p *Preparer) sweepLoop() {
	defer close(p.done)
	for {
		select {
		case <-p.stop:
			p.mu.Lock()
			p.sweepErr = errors.Join(p.sweepErr, p.sweepLocked())
			p.mu.Unlock()
			return
		case <-p.sweep:
			p.mu.Lock()
			p.sweepErr = errors.Join(p.sweepErr, p.sweepLocked())
			p.mu.Unlock()
		}
	}
}

func (p *Preparer) sweepLocked() error {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return err
	}
	type item struct {
		path string
		info os.FileInfo
	}
	var items []item
	for _, entry := range entries {
		// Sweep only this preparer's generated files, not unrelated directory data.
		if !strings.HasPrefix(entry.Name(), "src-") && !strings.HasPrefix(entry.Name(), "out-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			items = append(items, item{filepath.Join(p.dir, entry.Name()), info})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].info.ModTime().Equal(items[j].info.ModTime()) {
			return items[i].path > items[j].path
		}
		return items[i].info.ModTime().After(items[j].info.ModTime())
	})
	var result error
	for i := KeepLastFiles; i < len(items); i++ {
		if err := os.Remove(items[i].path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}
