package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMissingReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json")
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, Default()) {
		t.Fatalf("state=%+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("load created directory")
	}
}
func TestElixirStateAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	fixture := `{"version":1,"current_channel_id":"/media/Movies","channel_state":{"/media/Movies":{"file_path":"/media/Movies/a.mp4","offset_seconds":42},"/media/Photos":{"file_path":null,"offset_seconds":0}}}`
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.CurrentChannelID == nil || *s.CurrentChannelID != "/media/Movies" || s.ChannelState["/media/Movies"].OffsetSeconds != 42 {
		t.Fatalf("state=%+v", s)
	}
	output := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := Save(output, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(output)
	if err != nil || !reflect.DeepEqual(s, got) {
		t.Fatalf("state=%+v err=%v", got, err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v", info.Mode())
	}
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}
func TestInvalidStateIsNotReset(t *testing.T) {
	for _, text := range []string{``, `{`, `null`, `{}`, `{"version":2,"channel_state":{}}`, `{"version":1,"channel_state":null}`, `{"version":1,"channel_state":{"x":{"offset_seconds":-1}}}`, `{"version":1,"channel_state":{}} {}`, `{"version":1,"channel_state":{}} junk`} {
		t.Run(text, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("accepted invalid state")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != text {
				t.Fatal("load modified file")
			}
		})
	}
}
func TestInvalidSavePreservesOldState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bad := Default()
	bad.Version = 2
	if err := Save(path, bad); err == nil {
		t.Fatal("accepted invalid state")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("invalid save changed file")
	}
}
func TestSaveRenameFailureCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "destination")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Default()); err == nil {
		t.Fatal("replaced directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "destination" {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}
func TestLoadReadFailure(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("accepted directory as state")
	}
}
