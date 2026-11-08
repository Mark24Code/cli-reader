package state

import (
	"os"
	"path/filepath"
	"testing"

	"cli-reader/internal/book"
)

func TestStoreRoundTripAndToggles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")
	s := Load(path)
	bs := s.Book("key", "/books/a.epub", "A", "B")
	if !bs.ToggleChapterFavorite("ch1.xhtml", "Chapter 1") {
		t.Fatal("expected chapter favorite added")
	}
	if bs.IsChapterFavorite("ch1.xhtml") != true {
		t.Fatal("chapter favorite missing")
	}
	if !bs.TogglePositionFavorite("ch1.xhtml", "Chapter 1", 0.25) {
		t.Fatal("expected position favorite added")
	}
	if bs.TogglePositionFavorite("ch1.xhtml", "Chapter 1", 0.255) {
		t.Fatal("expected close position favorite to be toggled off")
	}
	if len(bs.Favorites) != 1 {
		t.Fatalf("favorites = %d", len(bs.Favorites))
	}
	bs.Last = book.Position{Href: "ch1.xhtml", Percent: 0.4}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}

	reloaded := Load(path)
	bs2 := reloaded.Book("key", "/books/a.epub", "A", "B")
	if len(bs2.Favorites) != 1 || !bs2.IsChapterFavorite("ch1.xhtml") {
		t.Fatalf("reloaded favorites = %#v", bs2.Favorites)
	}
	if bs2.Last.Percent != 0.4 {
		t.Fatalf("last percent = %v", bs2.Last.Percent)
	}
}

func TestDefaultPathUsesHomeDotDirectory(t *testing.T) {
	t.Setenv("CLI_EPUB_READER_STATE", "")
	home := t.TempDir()
	t.Setenv("HOME", home)        // unix
	t.Setenv("USERPROFILE", home) // windows
	want := filepath.Join(home, ".cli-reader", "state.json")
	if got := DefaultPath(); got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
	t.Setenv("CLI_EPUB_READER_STATE", filepath.Join(home, "custom.json"))
	if got := DefaultPath(); got != filepath.Join(home, "custom.json") {
		t.Fatalf("env override ignored: %q", got)
	}
}

func TestMigrateLegacyCopiesWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "old", "state.json")
	current := filepath.Join(dir, "new", "state.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"version":1,"books":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	migrateLegacyFile(current, legacy)
	if raw, err := os.ReadFile(current); err != nil || string(raw) != `{"version":1,"books":{}}` {
		t.Fatalf("migration failed: %q, %v", raw, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy file should be kept: %v", err)
	}

	// An existing file at the new location must win.
	if err := os.WriteFile(current, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	migrateLegacyFile(current, legacy)
	if raw, _ := os.ReadFile(current); string(raw) != "keep" {
		t.Fatalf("migration overwrote current state: %q", raw)
	}
}

func TestCorruptStoreBackedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if s.Warning() == nil {
		t.Fatal("expected warning")
	}
	s.Book("key", "/b.epub", "B", "")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}
