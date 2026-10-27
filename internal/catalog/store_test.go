package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func catalogMedia(t *testing.T, root, name, file string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func rejectCatalogWarning(t *testing.T) func(error) {
	return func(err error) { t.Helper(); t.Fatal(err) }
}

func TestCatalogMatchesReferenceJSONAndReusesCache(t *testing.T) {
	root := t.TempDir()
	media := catalogMedia(t, root, "channel", "one.mp4")
	path := filepath.Join(t.TempDir(), "catalog.json")
	c, err := LoadOrBuildCatalog(path, []string{root, root}, false, rejectCatalogWarning(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	roots := decoded["roots"].(map[string]any)
	channels := roots[root].(map[string]any)["channels"].(map[string]any)
	record := channels[filepath.Dir(media)].(map[string]any)
	if len(record) != 3 || record["path"] != filepath.Dir(media) || record["name"] != "channel" {
		t.Fatalf("unexpected cache record: %v", record)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	reused, err := LoadOrBuildCatalog(path, []string{root}, false, rejectCatalogWarning(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, reused) {
		t.Fatal("cached root was rescanned")
	}
	active, err := ActiveChannels(reused, []string{root, root})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Root != root || active[0].Media[0] != media {
		t.Fatal(active)
	}
	active[0].Media[0] = "mutated"
	if reused.Roots[root].Channels[filepath.Dir(media)].Media[0] != media {
		t.Fatal("active channels alias cached media")
	}
}

func TestCatalogExplicitScanRebuildsOnlySuppliedRoots(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	catalogMedia(t, root, "channel", "one.mp4")
	catalogMedia(t, other, "other", "old.mp4")
	path := filepath.Join(t.TempDir(), "catalog.json")
	if _, err := LoadOrBuildCatalog(path, []string{root, other}, false, rejectCatalogWarning(t)); err != nil {
		t.Fatal(err)
	}
	catalogMedia(t, root, "channel", "two.mp4")
	reused, err := LoadOrBuildCatalog(path, []string{root}, false, rejectCatalogWarning(t))
	if err != nil {
		t.Fatal(err)
	}
	active, err := ActiveChannels(reused, []string{root})
	if err != nil || len(active) != 1 || len(active[0].Media) != 1 {
		t.Fatal("implicit rescan")
	}
	rebuilt, err := LoadOrBuildCatalog(path, []string{root}, true, rejectCatalogWarning(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rebuilt.Roots) != 1 {
		t.Fatal("explicit scan retained unsupplied roots")
	}
	active, err = ActiveChannels(rebuilt, []string{root})
	if err != nil || len(active[0].Media) != 2 {
		t.Fatal("explicit scan did not refresh")
	}
}

func TestCatalogReportsMissingRootAndKeepsWorkingRoots(t *testing.T) {
	root := t.TempDir()
	catalogMedia(t, root, "channel", "one.mp4")
	warnings := 0
	path := filepath.Join(t.TempDir(), "catalog.json")
	c, err := LoadOrBuildCatalog(path, []string{root, filepath.Join(root, "missing")}, false, func(error) { warnings++ })
	if err != nil || warnings != 1 || len(c.Roots) != 1 {
		t.Fatalf("warnings=%d catalog=%+v err=%v", warnings, c, err)
	}
}

func TestCatalogRejectsMalformedCacheUnlessExplicitlyRescanning(t *testing.T) {
	root := t.TempDir()
	catalogMedia(t, root, "channel", "one.mp4")
	for _, text := range []string{`not json`, `{"version":2,"roots":{}}`, `{"version":1,"roots":null}`, `{"version":1,"roots":{}} {}`} {
		path := filepath.Join(t.TempDir(), "catalog.json")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrBuildCatalog(path, []string{root}, false, rejectCatalogWarning(t)); err == nil {
			t.Fatalf("accepted %q", text)
		}
		if _, err := LoadOrBuildCatalog(path, []string{root}, true, rejectCatalogWarning(t)); err != nil {
			t.Fatal(err)
		}
	}
}
