// Package store persists Pinstill screenshots and their thumbnails in a single
// directory: <id>.png for the original, <id>.thumb.jpg for the thumbnail, and
// index.json as the history index (newest first).
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"slices"

	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/echoVic/Pinstill/internal/diskio"
	"github.com/echoVic/Pinstill/internal/imagebudget"
)

const (
	indexFileName = "index.json"
	indexVersion  = 1
	pngExt        = ".png"
	thumbExt      = ".thumb.jpg"
	maxIndexBytes = 8 << 20

	// maxRetentionDays caps the age limit so multiplying by 24h cannot
	// overflow time.Duration for absurd configuration values. A retention of
	// "forever" is expressed as 0, and no screenshot is older than 100 years.
	maxRetentionDays = 100 * 365
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

	mu            sync.Mutex
	entries       []Entry // newest first
	pendingDelete []Entry // records whose files still need removal
	indexWriter   func(string, []byte, os.FileMode) error
}

// indexFile is the on-disk JSON shape of index.json.
type indexFile struct {
	Version       int     `json:"version"`
	Entries       []Entry `json:"entries"`
	PendingDelete []Entry `json:"pendingDelete,omitempty"`
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
	unlock, err := diskio.LockDir(absolute)
	if err != nil {
		return nil, err
	}
	defer unlock()
	raw, err := readIndexState(absolute)
	switch {
	case err == nil:
		rawEntries := raw.Entries
		entries := sanitizeEntries(rawEntries)
		pendingDelete := sanitizeEntries(raw.PendingDelete)
		for i := range entries {
			entry := &entries[i]
			if validThumbnail(filepath.Join(absolute, thumbName(entry.ID))) {
				entry.Thumb = thumbName(entry.ID)
				continue
			}
			data, readErr := diskio.ReadFile(filepath.Join(absolute, entry.File), imagebudget.MaxEncodedBytes)
			if readErr != nil {
				entry.Thumb = ""
				continue
			}
			source, decodeErr := imagebudget.DecodePNG(data)
			entry.Thumb = thumbName(entry.ID)
			if decodeErr != nil || !ensureThumbnail(absolute, *entry, source) {
				entry.Thumb = ""
			}
		}
		s.entries = entries
		s.pendingDelete = pendingDelete
		// Persist the cleaned index when entries were dropped, so a manually
		// edited index does not keep failing validation on every start.
		if !slices.Equal(entries, rawEntries) || !slices.Equal(pendingDelete, raw.PendingDelete) {
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
		s.pendingDelete = nil
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
	source, err := imagebudget.DecodePNG(pngData)
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
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return Entry{}, err
	}
	defer unlock()

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
		if diskio.Committed(err) {
			return entry, err
		}
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
	data, err := diskio.ReadFile(filepath.Join(s.dir, entry.File), imagebudget.MaxEncodedBytes)
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
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return err
	}
	defer unlock()

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
		_, cleanupErr := s.cleanupPendingDeletes()
		return cleanupErr
	}
	pending := appendPendingDeletes(s.pendingDelete, []Entry{removed})
	if err := s.writeIndexState(next, pending); err != nil {
		return err
	}
	_, cleanupErr := s.cleanupPendingDeletes()
	return cleanupErr
}

// Clear removes every screenshot and resets the index.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return err
	}
	defer unlock()

	removed := append([]Entry(nil), s.entries...)
	pending := appendPendingDeletes(s.pendingDelete, removed)
	if err := s.writeIndexState(nil, pending); err != nil {
		return err
	}
	_, err = s.cleanupPendingDeletes()
	return err
}

// Prune removes screenshots older than retentionDays (0 disables the age
// limit) and then the oldest screenshots until the total original PNG size is
// at most maxBytes (0 disables the size limit). The newest screenshot is
// always kept, even when it alone exceeds maxBytes.
func (s *Store) Prune(retentionDays int, maxBytes int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return 0, err
	}
	defer unlock()

	kept := make([]Entry, 0, len(s.entries))
	removed := make([]Entry, 0)
	var cutoff time.Time
	if retentionDays > 0 {
		days := min(retentionDays, maxRetentionDays)
		cutoff = time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	}
	for _, entry := range s.entries {
		if !cutoff.IsZero() && entry.CreatedAt.Before(cutoff) {
			removed = append(removed, entry)
			continue
		}
		kept = append(kept, entry)
	}

	if maxBytes > 0 && len(kept) > 0 {
		// Grow the newest-first prefix against the remaining budget. Summing
		// all sizes first can overflow for malformed positive index values.
		keepCount, total := 1, kept[0].Size
		for keepCount < len(kept) && total <= maxBytes && kept[keepCount].Size <= maxBytes-total {
			total += kept[keepCount].Size
			keepCount++
		}
		removed = append(removed, kept[keepCount:]...)
		kept = kept[:keepCount]
	}

	if len(removed) == 0 {
		cleaned, cleanupErr := s.cleanupPendingDeletes()
		return cleaned, cleanupErr
	}
	pending := appendPendingDeletes(s.pendingDelete, removed)
	if err := s.writeIndexState(kept, pending); err != nil {
		return 0, err
	}
	_, cleanupErr := s.cleanupPendingDeletes()
	return len(removed), cleanupErr
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

func appendPendingDeletes(existing, added []Entry) []Entry {
	if len(added) == 0 {
		return append([]Entry(nil), existing...)
	}
	pending := append([]Entry(nil), existing...)
	seen := make(map[string]struct{}, len(pending))
	for _, entry := range pending {
		seen[entry.ID] = struct{}{}
	}
	for _, entry := range added {
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		pending = append(pending, entry)
		seen[entry.ID] = struct{}{}
	}
	return pending
}

// cleanupPendingDeletes retries file removal after the index has already
// dropped the records. The pending list stays durable until every file is
// gone, so a transient filesystem error remains actionable on the next call.
// The caller must hold s.mu and the directory lock.
func (s *Store) cleanupPendingDeletes() (int, error) {
	if len(s.pendingDelete) == 0 {
		return 0, nil
	}
	pending := append([]Entry(nil), s.pendingDelete...)
	remaining := make([]Entry, 0, len(pending))
	cleaned := 0
	var firstErr error
	for i, entry := range pending {
		if err := s.removeFiles(entry); err != nil {
			firstErr = err
			remaining = append(remaining, pending[i:]...)
			break
		}
		cleaned++
	}
	if cleaned == 0 {
		return 0, firstErr
	}
	if err := s.writeIndexState(s.entries, remaining); err != nil {
		return cleaned, errors.Join(firstErr, err)
	}
	return cleaned, firstErr
}

// writeIndex atomically replaces index.json with entries. The caller must hold
// s.mu (Open calls it during construction before the store is shared).
func (s *Store) writeIndex(entries []Entry) error {
	return s.writeIndexState(entries, s.pendingDelete)
}

func (s *Store) writeIndexState(entries, pendingDelete []Entry) error {
	if entries == nil {
		entries = []Entry{}
	}
	if pendingDelete == nil {
		pendingDelete = []Entry{}
	}
	payload := indexFile{Version: indexVersion, Entries: entries, PendingDelete: pendingDelete}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("store: encode index: %w", err)
	}
	data = append(data, '\n')
	write := writeFileAtomic
	if s.indexWriter != nil {
		write = s.indexWriter
	}
	if err := write(filepath.Join(s.dir, indexFileName), data, 0o644); err != nil {
		if diskio.Committed(err) {
			s.entries = entries
			s.pendingDelete = pendingDelete
		}
		return fmt.Errorf("store: write index: %w", err)
	}
	s.entries = entries
	s.pendingDelete = pendingDelete
	return nil
}

func readIndexState(dir string) (indexFile, error) {
	data, err := diskio.ReadFile(filepath.Join(dir, indexFileName), maxIndexBytes)
	if err != nil {
		return indexFile{}, fmt.Errorf("store: read index: %w", err)
	}
	// Check the version before decoding records: a newer schema may use a
	// different record shape and must never be mistaken for corrupt old data.
	var index struct {
		Version       int             `json:"version"`
		Entries       json.RawMessage `json:"entries"`
		PendingDelete json.RawMessage `json:"pendingDelete"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return indexFile{}, fmt.Errorf("%w: %v", errCorruptIndex, err)
	}
	if index.Version > indexVersion {
		return indexFile{}, fmt.Errorf("store: index version %d is newer than supported version %d", index.Version, indexVersion)
	}
	if len(index.Entries) == 0 || bytes.Equal(bytes.TrimSpace(index.Entries), []byte("null")) {
		return indexFile{}, fmt.Errorf("%w: missing entries array", errCorruptIndex)
	}
	var state indexFile
	state.Version = index.Version
	if err := json.Unmarshal(index.Entries, &state.Entries); err != nil {
		return indexFile{}, fmt.Errorf("%w: %v", errCorruptIndex, err)
	}
	if len(index.PendingDelete) != 0 && !bytes.Equal(bytes.TrimSpace(index.PendingDelete), []byte("null")) {
		if err := json.Unmarshal(index.PendingDelete, &state.PendingDelete); err != nil {
			return indexFile{}, fmt.Errorf("%w: %v", errCorruptIndex, err)
		}
	}
	return state, nil
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
		data, err := diskio.ReadFile(pngPath, imagebudget.MaxEncodedBytes)
		if err != nil {
			continue
		}
		source, err := imagebudget.DecodePNG(data)
		if err != nil {
			continue
		}
		info, err := dirEntry.Info()
		if err != nil {
			continue
		}
		entry := Entry{
			ID:        id,
			CreatedAt: info.ModTime().Round(0),
			Width:     source.Bounds().Dx(),
			Height:    source.Bounds().Dy(),
			Size:      int64(len(data)),
			File:      pngName(id),
			Thumb:     thumbName(id),
		}
		if !ensureThumbnail(dir, entry, source) {
			entry.Thumb = ""
		}
		recovered = append(recovered, entry)
	}
	sortEntries(recovered)
	return recovered, nil
}

// ensureThumbnail makes sure the entry's thumbnail file exists, rebuilding it
// from the PNG when needed. It reports whether a thumbnail is available.
func ensureThumbnail(dir string, entry Entry, source image.Image) bool {
	thumbPath := filepath.Join(dir, entry.Thumb)
	if validThumbnail(thumbPath) {
		return true
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
	return diskio.WriteFileAtomic(path, data, mode)
}

// removeIfExists deletes path and treats a missing file as success.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// lockAndRefresh is called with s.mu held. The OS lock covers the complete
// transaction so independently opened handles cannot overwrite each other.
func (s *Store) lockAndRefresh() (func(), error) {
	unlock, err := diskio.LockDir(s.dir)
	if err != nil {
		return nil, err
	}
	current, err := readIndexState(s.dir)
	if err != nil {
		unlock()
		return nil, err
	}
	s.entries = sanitizeEntries(current.Entries)
	s.pendingDelete = sanitizeEntries(current.PendingDelete)
	return unlock, nil
}

func validThumbnail(path string) bool {
	data, err := diskio.ReadFile(path, imagebudget.MaxEncodedBytes)
	if err != nil {
		return false
	}
	// Reject oversized thumbnails before decoding their full pixel data.
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > thumbnailMaxEdge || config.Height > thumbnailMaxEdge {
		return false
	}
	_, format, err := imagebudget.Decode(data)
	return err == nil && format == "jpeg"
}
