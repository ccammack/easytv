package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// These types preserve the reference catalog.json layout. Root is reconstructed
// on active Channel values, not added to individual persisted channel records.
type Catalog struct {
	Version int                    `json:"version"`
	Roots   map[string]CatalogRoot `json:"roots"`
}
type CatalogRoot struct {
	Channels map[string]CatalogChannel `json:"channels"`
}
type CatalogChannel struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Media []string `json:"media"`
}

func DefaultCatalog() Catalog { return Catalog{Version: 1, Roots: make(map[string]CatalogRoot)} }
func (c Catalog) Validate() error {
	if c.Version != 1 || c.Roots == nil {
		return errors.New("invalid catalog version or roots")
	}
	for root, entry := range c.Roots {
		if root == "" || entry.Channels == nil {
			return fmt.Errorf("invalid catalog root %q", root)
		}
		for id, ch := range entry.Channels {
			if id == "" || ch.Path == "" || ch.Name == "" || ch.Media == nil {
				return fmt.Errorf("invalid cached channel %q", id)
			}
			for _, path := range ch.Media {
				if path == "" {
					return fmt.Errorf("empty cached media path in %q", id)
				}
			}
		}
	}
	return nil
}
func LoadCatalog(path string) (Catalog, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultCatalog(), nil
	}
	if err != nil {
		return Catalog{}, err
	}
	defer file.Close()
	var c Catalog
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Catalog{}, errors.New("catalog contains trailing data")
	}
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}
func SaveCatalog(path string, c Catalog) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".catalog-*.tmp")
	if err != nil {
		return err
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func normalizeRoots(roots []string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		if !seen[absolute] {
			seen[absolute] = true
			result = append(result, absolute)
		}
	}
	return result, nil
}

// LoadOrBuildCatalog implements the reference --scan policy: rebuild supplied
// roots on explicit scan; otherwise reuse cached roots and scan missing ones.
// Root scan errors are reported and skipped, as in the reference. Persistence
// and invalid-cache errors are returned, not silently replaced with defaults.
func LoadOrBuildCatalog(path string, roots []string, scan bool, report func(error)) (Catalog, error) {
	normalized, err := normalizeRoots(roots)
	if err != nil {
		return Catalog{}, err
	}
	if report == nil {
		return Catalog{}, errors.New("catalog error reporter required")
	}
	c := DefaultCatalog()
	if !scan {
		c, err = LoadCatalog(path)
		if err != nil {
			return Catalog{}, err
		}
	}
	changed := scan
	for _, root := range normalized {
		if !scan {
			if _, ok := c.Roots[root]; ok {
				continue
			}
		}
		channels, err := ScanRoot(root)
		if err != nil {
			report(err)
			continue
		}
		entry := CatalogRoot{Channels: make(map[string]CatalogChannel)}
		for _, ch := range channels {
			entry.Channels[ch.Path] = CatalogChannel{Name: ch.Name, Path: ch.Path, Media: append([]string{}, ch.Media...)}
		}
		c.Roots[root] = entry
		changed = true
	}
	if changed {
		if err := SaveCatalog(path, c); err != nil {
			return Catalog{}, fmt.Errorf("save catalog: %w", err)
		}
	}
	return c, nil
}

func ActiveChannels(c Catalog, roots []string) ([]Channel, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	normalized, err := normalizeRoots(roots)
	if err != nil {
		return nil, err
	}
	result := make([]Channel, 0)
	for _, root := range normalized {
		for _, ch := range c.Roots[root].Channels {
			result = append(result, Channel{Root: root, Name: ch.Name, Path: ch.Path, Media: append([]string{}, ch.Media...)})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Root != result[j].Root {
			return result[i].Root < result[j].Root
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Path < result[j].Path
	})
	return result, nil
}
