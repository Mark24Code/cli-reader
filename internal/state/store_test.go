package state

import (
	"os"
	"path/filepath"
	"testing"

	"claude-epub-reader/internal/epub"
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
	bs.Last = epub.Position{Href: "ch1.xhtml", Percent: 0.4}
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
