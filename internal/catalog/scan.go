package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrRescanInProgress = errors.New("rescan already in progress")

// Channel is a nonempty immediate child directory of a media root.
// Path is the stable channel identity used by the existing application.
type Channel struct {
	Root  string   `json:"root"`
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Media []string `json:"media"`
}

var mediaExtensions = extensionSet(`mp3 flac m4a m4b aac ac3 ogg oga opus wav aiff alac ape amr wma wv tta mp1 mp2 mpa mid midi xm it s3m mod ra rmi spx voc rm mp4 m4v mkv mov avi webm flv wmv mpg mpeg mpv m1v m2v ts m2ts mts vob ogv ogm 3gp 3g2 asf divx dv f4v mxf nuv rmvb tod trp vro jpg jpeg jpe jfif jif png gif bmp tif tiff webp ico pbm pgm ppm pnm tga`)

func extensionSet(text string) map[string]bool {
	result := make(map[string]bool)
	for _, ext := range strings.Fields(text) {
		result["."+ext] = true
	}
	return result
}

// ScanRoot collects sorted media paths without opening media contents.
// Hidden entries and symbolic links are skipped. Read errors are returned,
// never silently converted into a successful partial catalog.
func ScanRoot(root string) ([]Channel, error) {
	return scanRoot(context.Background(), root)
}

func scanRoot(ctx context.Context, root string) ([]Channel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return nil, fmt.Errorf("read root %q: %w", absolute, err)
	}
	channels := make([]Channel, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() {
			continue
		}
		channel := Channel{Root: absolute, Name: entry.Name(), Path: filepath.Join(absolute, entry.Name()), Media: make([]string, 0)}
		err := filepath.WalkDir(channel.Path, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if path != channel.Path && strings.HasPrefix(entry.Name(), ".") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
				return nil
			}
			if !mediaExtensions[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				channel.Media = append(channel.Media, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan channel %q: %w", channel.Path, err)
		}
		if len(channel.Media) > 0 {
			sort.Strings(channel.Media)
			channels = append(channels, channel)
		}
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].Path < channels[j].Path })
	return channels, nil
}

// Scan builds a complete replacement without saving it. Unlike startup's
// best-effort scan, a live refresh must not replace a working catalog on error.
func Scan(ctx context.Context, roots []string) (Catalog, error) {
	normalized, err := normalizeRoots(roots)
	if err != nil {
		return Catalog{}, err
	}
	if len(normalized) == 0 {
		return Catalog{}, errors.New("media roots required")
	}
	result := DefaultCatalog()
	for _, root := range normalized {
		channels, err := scanRoot(ctx, root)
		if err != nil {
			return Catalog{}, err
		}
		entry := CatalogRoot{Channels: make(map[string]CatalogChannel)}
		for _, ch := range channels {
			entry.Channels[ch.Path] = CatalogChannel{Name: ch.Name, Path: ch.Path, Media: ch.Media}
		}
		result.Roots[root] = entry
	}
	return result, ctx.Err()
}
