// Package store persists Pinstill screenshots and their thumbnails in a single
// directory: <id>.png for the original, <id>.thumb.jpg for the thumbnail, and
// index.json as the history index (newest first).
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	indexFileName = "index.json"
	indexVersion  = 1
	pngExt        = ".png"
	thumbExt      = ".thumb.jpg"
)

// errCorruptIndex marks an index.json that cannot be trusted. It triggers a
// rebuild from the PNG files on disk instead of failing Open.
var errCorruptIndex = errors.New("store: corrupt index")

// Entry describes one persisted screenshot. File and Thumb are relative to the
// store directory.
type Entry struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Size      int64     `json:"size"`
	File      string    `json:"file"`
	Thumb     string    `json:"thumb"`
}

// Store is a concurrency-safe screenshot history directory.
type Store struct {
	dir string

	mu      sync.Mutex
	entries []Entry // newest first
}

// indexFile is the on-disk JSON shape of index.json.
type indexFile struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Open creates dir when needed and loads the history index. A missing or
// corrupt index is rebuilt by scanning the PNG files in dir, so screenshots
// whose index was lost are recovered rather than orphaned.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty directory")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("store: resolve directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("store: create directory: %w", err)
	}

	s := &Store{dir: absolute}
	raw, err := readIndex(absolute)
	switch {
	case err == nil:
		entries := sanitizeEntries(raw)
		s.entries = entries
		// Persist the cleaned index when entries were dropped, so a manually
		// edited index does not keep failing validation on every start.
		if len(entries) != len(raw) {
			if err := s.writeIndex(entries); err != nil {
				return nil, err
			}
		}
		return s, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, errCorruptIndex):
		recovered, recoverErr := recoverEntries(absolute)
		if recoverErr != nil {
			return nil, recoverErr
		}
		s.entries = recovered
		if err := s.writeIndex(recovered); err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, err
	}
}

// Dir returns the absolute directory the store writes to.
func (s *Store) Dir() string { return s.dir }

// Save persists a PNG screenshot and its thumbnail, then updates the index.
// createdAt is truncated to the index representation; a zero value means now.
func (s *Store) Save(pngData []byte, createdAt time.Time) (Entry, error) {
	if len(pngData) == 0 {
		return Entry{}, errors.New("store: empty image")
	}
	source, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return Entry{}, fmt.Errorf("store: decode PNG: %w", err)
	}
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	if width <= 0 || height <= 0 {
		return Entry{}, errors.New("store: PNG has no pixels")
	}
	thumbnail, err := encodeThumbnail(source)
	if err != nil {
		return Entry{}, err
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	createdAt = createdAt.Round(0) // strip the monotonic clock reading

	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.uniqueIDLocked(createdAt)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{
		ID:        id,
		CreatedAt: createdAt,
		Width:     width,
		Height:    height,
		Size:      int64(len(pngData)),
		File:      pngName(id),
		Thumb:     thumbName(id),
	}

	pngPath := filepath.Join(s.dir, entry.File)
	thumbPath := filepath.Join(s.dir, entry.Thumb)
	if err := writeFileAtomic(pngPath, pngData, 0o644); err != nil {
		return Entry{}, fmt.Errorf("store: write screenshot: %w", err)
	}
	if err := writeFileAtomic(thumbPath, thumbnail, 0o644); err != nil {
		_ = removeIfExists(pngPath)
		return Entry{}, fmt.Errorf("store: write thumbnail: %w", err)
	}

	next := make([]Entry, 0, len(s.entries)+1)
	next = append(next, s.entries...)
	next = append(next, entry)
	sortEntries(next)
	if err := s.writeIndex(next); err != nil {
		_ = removeIfExists(pngPath)
		_ = removeIfExists(thumbPath)
		return Entry{}, err
	}
	s.entries = next
	return entry, nil
}

// List returns a copy of the history, newest first.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make([]Entry, len(s.entries))
	copy(entries, s.entries)
	return entries
}

// Get returns the entry for id.
func (s *Store) Get(id string) (Entry, bool) {
	if !validID(id) {
		return Entry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

// ReadPNG returns the original PNG bytes for id.
func (s *Store) ReadPNG(id string) ([]byte, error) {
	entry, ok := s.Get(id)
	if !ok {
		return nil, fmt.Errorf("store: screenshot %q not found", id)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, entry.File))
	if err != nil {
		return nil, fmt.Errorf("store: read screenshot %q: %w", id, err)
	}
	return data, nil
}

// Delete removes a screenshot and its thumbnail. Deleting an unknown or
// malformed ID is a no-op so the history UI can retry safely.
func (s *Store) Delete(id string) error {
	if !validID(id) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make([]Entry, 0, len(s.entries))
	var removed Entry
	found := false
	for _, entry := range s.entries {
		if entry.ID == id {
			removed = entry
			found = true
			continue
		}
		next = append(next, entry)
	}
	if !found {
		return nil
	}
	if err := s.writeIndex(next); err != nil {
		return err
	}
	s.entries = next
	return s.removeFiles(removed)
}

// Clear removes every screenshot and resets the index.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := s.entries
	if err := s.writeIndex(nil); err != nil {
		return err
	}
	s.entries = nil
	var firstErr error
	for _, entry := range removed {
		if err := s.removeFiles(entry); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Prune removes screenshots older than retentionDays (0 disables the age
// limit) and then the oldest screenshots until the total original PNG size is
// at most maxBytes (0 disables the size limit). The newest screenshot is
// always kept, even when it alone exceeds maxBytes.
func (s *Store) Prune(retentionDays int, maxBytes int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]Entry, 0, len(s.entries))
	removed := make([]Entry, 0)
	var cutoff time.Time
	if retentionDays > 0 {
		cutoff = time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	}
	for _, entry := range s.entries {
		if !cutoff.IsZero() && entry.CreatedAt.Before(cutoff) {
			removed = append(removed, entry)
			continue
		}
		kept = append(kept, entry)
	}

	if maxBytes > 0 && len(kept) > 0 {
		var total int64
		for _, entry := range kept {
			total += entry.Size
		}
		if total > maxBytes {
			keepCount := len(kept)
			for keepCount > 1 && total > maxBytes {
				keepCount--
				total -= kept[keepCount].Size
			}
			removed = append(removed, kept[keepCount:]...)
			kept = kept[:keepCount]
		}
	}

	if len(removed) == 0 {
		return 0, nil
	}
	if err := s.writeIndex(kept); err != nil {
		return 0, err
	}
	s.entries = kept
	var firstErr error
	for _, entry := range removed {
		if err := s.removeFiles(entry); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(removed), firstErr
}

// uniqueIDLocked allocates an ID that neither the index nor the directory
// already uses. The caller must hold s.mu.
func (s *Store) uniqueIDLocked(createdAt time.Time) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		id, err := newID(createdAt)
		if err != nil {
			return "", err
		}
		if s.hasIDLocked(id) {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.dir, pngName(id))); err == nil {
			continue
		}
		return id, nil
	}
	return "", errors.New("store: could not allocate a unique screenshot ID")
}

// hasIDLocked reports whether id is already indexed. The caller must hold s.mu.
func (s *Store) hasIDLocked(id string) bool {
	for _, entry := range s.entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

// removeFiles deletes the entry's screenshot and thumbnail, ignoring files that
// already vanished.
func (s *Store) removeFiles(entry Entry) error {
	for _, name := range []string{entry.File, entry.Thumb} {
		if name == "" {
			continue
		}
		if err := removeIfExists(filepath.Join(s.dir, name)); err != nil {
			return fmt.Errorf("store: remove %q: %w", name, err)
		}
	}
	return nil
}

// writeIndex atomically replaces index.json with entries. The caller must hold
// s.mu (Open calls it during construction before the store is shared).
func (s *Store) writeIndex(entries []Entry) error {
	if entries == nil {
		entries = []Entry{}
	}
	payload := indexFile{Version: indexVersion, Entries: entries}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode index: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(filepath.Join(s.dir, indexFileName), data, 0o644); err != nil {
		return fmt.Errorf("store: write index: %w", err)
	}
	return nil
}

// readIndex loads index.json, distinguishing a missing file from a corrupt one.
func readIndex(dir string) ([]Entry, error) {
	data, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		return nil, fmt.Errorf("store: read index: %w", err)
	}
	var index indexFile
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("%w: %v", errCorruptIndex, err)
	}
	if index.Version > indexVersion {
		return nil, fmt.Errorf("store: index version %d is newer than supported version %d", index.Version, indexVersion)
	}
	return index.Entries, nil
}

// sanitizeEntries drops entries that do not match the frozen ID/file contract,
// removes duplicates, and sorts newest first.
func sanitizeEntries(entries []Entry) []Entry {
	cleaned := make([]Entry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !validID(entry.ID) {
			continue
		}
		if entry.File != pngName(entry.ID) {
			continue
		}
		if entry.Thumb != "" && entry.Thumb != thumbName(entry.ID) {
			continue
		}
		if entry.Width <= 0 || entry.Height <= 0 || entry.Size <= 0 {
			continue
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			continue
		}
		seen[entry.ID] = struct{}{}
		entry.CreatedAt = entry.CreatedAt.Round(0)
		cleaned = append(cleaned, entry)
	}
	sortEntries(cleaned)
	return cleaned
}

// recoverEntries rebuilds the index from <id>.png files found on disk.
func recoverEntries(dir string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: scan directory: %w", err)
	}

	recovered := make([]Entry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}
		name := dirEntry.Name()
		if !strings.HasSuffix(name, pngExt) {
			continue
		}
		id := strings.TrimSuffix(name, pngExt)
		if !validID(id) {
			continue
		}
		pngPath := filepath.Join(dir, name)
		data, err := os.ReadFile(pngPath)
		if err != nil {
			continue
		}
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width <= 0 || config.Height <= 0 {
			continue
		}
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}
		entry := Entry{
			ID:        id,
			CreatedAt: info.ModTime().Round(0),
			Width:     config.Width,
			Height:    config.Height,
			Size:      int64(len(data)),
			File:      pngName(id),
			Thumb:     thumbName(id),
		}
		if !ensureThumbnail(dir, entry, data) {
			entry.Thumb = ""
		}
		recovered = append(recovered, entry)
	}
	sortEntries(recovered)
	return recovered, nil
}

// ensureThumbnail makes sure the entry's thumbnail file exists, rebuilding it
// from the PNG when needed. It reports whether a thumbnail is available.
func ensureThumbnail(dir string, entry Entry, pngData []byte) bool {
	thumbPath := filepath.Join(dir, entry.Thumb)
	if info, err := os.Stat(thumbPath); err == nil && info.Size() > 0 {
		return true
	}
	source, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return false
	}
	thumbnail, err := encodeThumbnail(source)
	if err != nil {
		return false
	}
	if err := writeFileAtomic(thumbPath, thumbnail, 0o644); err != nil {
		return false
	}
	return true
}

// sortEntries orders entries newest first, breaking ties by ID so the order is
// deterministic for screenshots taken in the same second.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].ID > entries[j].ID
		}
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
}

// writeFileAtomic writes data to path via a temporary file in the same
// directory followed by a rename, so a crash can never leave a partial file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	discard := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if _, err := temp.Write(data); err != nil {
		discard()
		return err
	}
	if err := temp.Sync(); err != nil {
		discard()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Chmod(tempName, mode); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

// removeIfExists deletes path and treats a missing file as success.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
