package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cli-epub-reader/internal/epub"
)

const (
	Version = 1

	FavoriteChapter  = "chapter"
	FavoritePosition = "position"
)

type Favorite struct {
	Kind         string  `json:"kind"`
	Href         string  `json:"href"`
	ChapterTitle string  `json:"chapter_title,omitempty"`
	Percent      float64 `json:"percent,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

type BookState struct {
	Path      string        `json:"path"`
	Title     string        `json:"title,omitempty"`
	Author    string        `json:"author,omitempty"`
	Last      epub.Position `json:"last_position"`
	Favorites []Favorite    `json:"favorites,omitempty"`
}

type diskState struct {
	Version int                   `json:"version"`
	Books   map[string]*BookState `json:"books"`
}

type Store struct {
	Path string

	mu      sync.Mutex
	data    diskState
	warning error
	corrupt bool
}

// DefaultPath returns ~/.cli-epub-reader/state.json, honouring
// CLI_EPUB_READER_STATE for an explicit override.
func DefaultPath() string {
	if p := strings.TrimSpace(os.Getenv("CLI_EPUB_READER_STATE")); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cli-epub-reader", "state.json")
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "cli-epub-reader", "state.json")
	}
	return filepath.Join(".", ".cli-epub-reader", "state.json")
}

// legacyPath is the config-directory location used by older releases.
func legacyPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "cli-epub-reader", "state.json")
}

// MigrateLegacy copies an old state file to the default path the first time
// the new location is used. The legacy file is left in place, so nothing is
// lost if the user wants to go back. It never overwrites an existing file.
func MigrateLegacy(path string) {
	migrateLegacyFile(path, legacyPath())
}

func migrateLegacyFile(path, legacy string) {
	if path == "" || legacy == "" || legacy == path {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	raw, err := os.ReadFile(legacy)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		_ = os.Remove(path)
	}
}

// Load reads a store. A missing file is not an error. Corrupt JSON yields an
// empty store plus Warning; the original file is backed up on the next Save.
func Load(path string) *Store {
	s := &Store{
		Path: path,
		data: diskState{Version: Version, Books: make(map[string]*BookState)},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.warning = err
		}
		return s
	}
	var ds diskState
	if err := json.Unmarshal(raw, &ds); err != nil {
		s.warning = fmt.Errorf("收藏/进度文件损坏: %w", err)
		s.corrupt = true
		return s
	}
	if ds.Books == nil {
		ds.Books = make(map[string]*BookState)
	}
	if ds.Version == 0 {
		ds.Version = Version
	}
	s.data = ds
	for _, bs := range s.data.Books {
		normalizeBookState(bs)
	}
	return s
}

func normalizeBookState(bs *BookState) {
	if bs == nil {
		return
	}
	if bs.Last.Percent < 0 {
		bs.Last.Percent = 0
	}
	if bs.Last.Percent > 1 {
		bs.Last.Percent = 1
	}
	for i := range bs.Favorites {
		f := &bs.Favorites[i]
		if f.Kind != FavoriteChapter && f.Kind != FavoritePosition {
			f.Kind = FavoritePosition
		}
		if f.Percent < 0 {
			f.Percent = 0
		}
		if f.Percent > 1 {
			f.Percent = 1
		}
	}
}

func (s *Store) Warning() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.warning
}

// BookKey returns a stable key for a book path.
func BookKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:12])
}

// Book returns (creating if needed) state for a book.
func (s *Store) Book(key, path, title, author string) *BookState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Books == nil {
		s.data.Books = make(map[string]*BookState)
	}
	bs := s.data.Books[key]
	if bs == nil {
		bs = &BookState{Path: path}
		s.data.Books[key] = bs
	}
	bs.Path = path
	bs.Title = title
	bs.Author = author
	normalizeBookState(bs)
	return bs
}

// Save atomically writes the state file.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Path == "" {
		return nil
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if s.corrupt {
		backup := s.Path + ".corrupt"
		_ = os.Remove(backup)
		if err := os.Rename(s.Path, backup); err != nil {
			return fmt.Errorf("备份损坏的状态文件失败: %w", err)
		}
		s.corrupt = false
	}
	s.data.Version = Version
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// TogglePositionFavorite adds or removes a position favorite close to percent.
func (bs *BookState) TogglePositionFavorite(href, title string, percent float64) bool {
	for i, f := range bs.Favorites {
		if f.Kind == FavoritePosition && f.Href == href && abs(f.Percent-percent) <= 0.02 {
			bs.Favorites = append(bs.Favorites[:i], bs.Favorites[i+1:]...)
			return false
		}
	}
	bs.Favorites = append(bs.Favorites, Favorite{
		Kind:         FavoritePosition,
		Href:         href,
		ChapterTitle: title,
		Percent:      clamp01(percent),
		CreatedAt:    time.Now().Format(time.RFC3339),
	})
	return true
}

// ToggleChapterFavorite adds or removes a whole-chapter favorite.
func (bs *BookState) ToggleChapterFavorite(href, title string) bool {
	for i, f := range bs.Favorites {
		if f.Kind == FavoriteChapter && f.Href == href {
			bs.Favorites = append(bs.Favorites[:i], bs.Favorites[i+1:]...)
			return false
		}
	}
	bs.Favorites = append(bs.Favorites, Favorite{
		Kind:         FavoriteChapter,
		Href:         href,
		ChapterTitle: title,
		CreatedAt:    time.Now().Format(time.RFC3339),
	})
	return true
}

func (bs *BookState) IsChapterFavorite(href string) bool {
	for _, f := range bs.Favorites {
		if f.Kind == FavoriteChapter && f.Href == href {
			return true
		}
	}
	return false
}

func (bs *BookState) RemoveFavorite(index int) bool {
	if index < 0 || index >= len(bs.Favorites) {
		return false
	}
	bs.Favorites = append(bs.Favorites[:index], bs.Favorites[index+1:]...)
	return true
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
