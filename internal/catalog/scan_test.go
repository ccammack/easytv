package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func put(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanRootConventions(t *testing.T) {
	root := t.TempDir()
	z := put(t, root, "B/z.MP4")
	a := put(t, root, "B/nested/a.jpg")
	music := put(t, root, "A/song.flac")
	for _, name := range []string{"loose.mp4", ".hidden/no.mp4", "B/.secret.mp4", "B/.cache/no.jpg", "B/readme.txt", "Empty/readme.md"} {
		put(t, root, name)
	}
	got, err := ScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	media := []string{z, a}
	sortPaths(media)
	want := []Channel{
		{Root: root, Name: "A", Path: filepath.Join(root, "A"), Media: []string{music}},
		{Root: root, Name: "B", Path: filepath.Join(root, "B"), Media: media},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func sortPaths(paths []string) {
	if paths[0] > paths[1] {
		paths[0], paths[1] = paths[1], paths[0]
	}
}

func TestEmptyRoot(t *testing.T) {
	got, err := ScanRoot(t.TempDir())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("channels=%v err=%v", got, err)
	}
}
func TestMissingRoot(t *testing.T) {
	_, err := ScanRoot(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error=%v", err)
	}
}
func TestFileIsNotRoot(t *testing.T) {
	file := put(t, t.TempDir(), "file.mp4")
	if _, err := ScanRoot(file); err == nil {
		t.Fatal("file accepted as directory")
	}
}
func TestSymlinksAreSkipped(t *testing.T) {
	root := t.TempDir()
	keep := put(t, root, "Channel/keep.mp4")
	outside := t.TempDir()
	target := put(t, outside, "outside.mp4")
	links := map[string]string{"linked-channel": outside, "Channel/linked.mp4": target, "Channel/cycle": root}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ScanRoot(root)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Media, []string{keep}) {
		t.Fatalf("channels=%v err=%v", got, err)
	}
}
func TestRelativeRootIsExpanded(t *testing.T) {
	root, err := os.MkdirTemp(".", "catalog-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	put(t, root, "Channel/video.mp4")
	got, err := ScanRoot(root)
	if err != nil || len(got) != 1 || !filepath.IsAbs(got[0].Path) {
		t.Fatalf("channels=%v err=%v", got, err)
	}
}
func TestExtensionSet(t *testing.T) {
	for _, ext := range []string{".mp4", ".jpg", ".flac", ".midi", ".tga"} {
		if !mediaExtensions[ext] {
			t.Errorf("missing %s", ext)
		}
	}
	for _, ext := range []string{".txt", ".pdf", ""} {
		if mediaExtensions[ext] {
			t.Errorf("unexpected %s", ext)
		}
	}
}
