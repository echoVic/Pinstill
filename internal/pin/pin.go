// Package pin persists the state of Pinstill's desktop pins: the image files
// ("<id>.png") and the window geometry/opacity of every pin ("pins.json") live
// in one directory under the application's user data. Pins are restored on the
// next launch from that state.
//
// The package is stdlib-only and safe for concurrent use.
package pin

import (
	"encoding/json"
	"errors"
	"fmt"

	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/echoVic/Pinstill/internal/diskio"
	"github.com/echoVic/Pinstill/internal/imagebudget"
)

const (
	indexFileName = "pins.json"
	indexVersion  = 1
	imageExt      = ".png"
	maxIndexBytes = 8 << 20

	// minPinSize is the smallest persisted window size in DIPs.
	minPinSize = 1
	// minOpacity and maxOpacity bound the persisted window opacity (percent).
	minOpacity = 10.0
	maxOpacity = 100.0

	maxIDLength = 128
)

var (
	// ErrInvalidID reports a pin ID that is not a safe file name.
	ErrInvalidID = errors.New("pin: invalid id")
	// ErrInvalidImage reports bytes that are not a valid PNG image.
	ErrInvalidImage = errors.New("pin: invalid image")
	// ErrInvalidFile reports a state file name that is not safe to join with
	// the store directory.
	ErrInvalidFile = errors.New("pin: invalid image file name")

	// errCorruptIndex marks a pins.json that cannot be decoded. It is
	// tolerated by Open, which starts from an empty state instead.
	errCorruptIndex = errors.New("pin: corrupt pin index")
)

// State is the persisted state of one desktop pin.
type State struct {
	// ID is the filename-safe, stable identifier of the pin.
	ID string `json:"id"`
	// File is the image file name relative to Dir(), usually "<id>.png".
	File string `json:"file"`
	// X, Y is the window origin in screen DIPs (may be negative on displays
	// left of or above the primary one).
	X int `json:"x"`
	Y int `json:"y"`
	// Width, Height is the window size in DIPs, never below minPinSize.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Opacity is the window opacity in percent, 10..100.
	Opacity float64 `json:"opacity"`
	// CreatedAt is the creation time, used to keep a stable restore order.
	CreatedAt time.Time `json:"createdAt"`
	// Archived pins retain their image and state but never reopen automatically.
	Archived bool `json:"archived,omitempty"`
}

// Store persists pin images and their state in one directory.
type Store struct {
	dir string

	mu     sync.Mutex
	states []State // sorted ascending by CreatedAt, then ID
}

// indexFile is the on-disk shape of pins.json.
type indexFile struct {
	Version int     `json:"version"`
	Pins    []State `json:"pins"`
}

// idPattern allows only characters that are safe in file names on every
// supported platform and rules out path separators, leading dots and spaces.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// filePattern is idPattern plus the mandatory .png suffix.
var filePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.png$`)

// reservedWindowsNames are the DOS device names that cannot be used as file
// names on Windows, even with an extension.
var reservedWindowsNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// Open creates dir if needed and loads the persisted pins. A missing or
// corrupt pins.json is tolerated: the store starts empty while the image files
// are left in place. Entries whose ID, file name or image is invalid or missing
// are dropped, and the cleaned index is written back.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("pin: empty directory")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("pin: resolve directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("pin: create directory: %w", err)
	}

	s := &Store{dir: absolute}
	unlock, err := diskio.LockDir(absolute)
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Leftovers from a crash during an atomic write are never useful.
	removeStaleTempFiles(absolute)
	states, err := readIndex(absolute)
	switch {
	case err == nil:
		cleaned, changed := s.sanitizeStates(states)
		s.states = cleaned
		if changed {
			if err := s.writeIndex(cleaned); err != nil {
				return nil, err
			}
		}
		return s, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, errCorruptIndex):
		s.states = nil
		// Replace a missing/corrupt index with a valid empty one. The image
		// files stay where they are; dropping the broken state never deletes
		// user images.
		if err := s.writeIndex(nil); err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, err
	}
}

// Dir returns the absolute directory the store keeps its files in.
func (s *Store) Dir() string { return s.dir }

// List returns a copy of the persisted pin states, ordered by CreatedAt
// ascending and then by ID (oldest first, so new pins are appended at the end).
func (s *Store) List() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make([]State, len(s.states))
	copy(states, s.states)
	return states
}

// SaveImage validates pngBytes and durably writes them as "<id>.png" in the
// store directory, returning the file name relative to Dir(). Writing goes
// through a temporary file and a rename, so a crash cannot leave a truncated
// image behind. An existing image for the same ID is replaced.
func (s *Store) SaveImage(id string, pngBytes []byte) (string, error) {
	validID, err := validateID(id)
	if err != nil {
		return "", err
	}
	if len(pngBytes) == 0 {
		return "", fmt.Errorf("%w: empty data", ErrInvalidImage)
	}
	if _, err := imagebudget.DecodePNG(pngBytes); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	file, err := validateFile(validID + imageExt)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := writeFileAtomic(filepath.Join(s.dir, file), pngBytes, 0o644); err != nil {
		return "", fmt.Errorf("pin: write image %q: %w", file, err)
	}
	return file, nil
}

// ReadImage returns the PNG bytes of the image referenced by st.File, or by
// st.ID when File is empty.
func (s *Store) ReadImage(st State) ([]byte, error) {
	file := st.File
	if file == "" {
		id, err := validateID(st.ID)
		if err != nil {
			return nil, err
		}
		file = id + imageExt
	}
	file, err := validateFile(file)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := diskio.ReadFile(filepath.Join(s.dir, file), imagebudget.MaxEncodedBytes)
	if err != nil {
		return nil, fmt.Errorf("pin: read image %q: %w", file, err)
	}
	if _, err := imagebudget.DecodePNG(data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	return data, nil
}

// Upsert validates and stores st, replacing the existing state with the same
// ID. Missing CreatedAt (zero) and File (empty) are taken from the stored state
// or defaulted (time.Now, "<id>.png"). Window size is raised to minPinSize and
// opacity is clamped to 10..100 (0 means "unset" and becomes 100). The stored
// value is returned.
func (s *Store) Upsert(st State) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return State{}, err
	}
	defer unlock()

	id, err := validateID(st.ID)
	if err != nil {
		return State{}, err
	}
	st.ID = id
	existing, found := s.findLocked(id)

	if st.File == "" {
		if found {
			st.File = existing.File
		} else {
			st.File = id + imageExt
		}
	}
	file, err := validateFile(st.File)
	if err != nil {
		return State{}, err
	}
	st.File = file
	st.Width = max(st.Width, minPinSize)
	st.Height = max(st.Height, minPinSize)
	st.Opacity = normalizeOpacity(st.Opacity)
	if st.CreatedAt.IsZero() {
		if found {
			st.CreatedAt = existing.CreatedAt
		} else {
			st.CreatedAt = time.Now()
		}
	}
	st.CreatedAt = st.CreatedAt.Round(0)

	next := make([]State, 0, len(s.states)+1)
	replaced := false
	for _, current := range s.states {
		if current.ID == id {
			next = append(next, st)
			replaced = true
			continue
		}
		next = append(next, current)
	}
	if !replaced {
		next = append(next, st)
	}
	sortStates(next)
	if err := s.writeIndex(next); err != nil {
		if diskio.Committed(err) {
			return st, err
		}
		return State{}, err
	}
	s.states = next
	return st, nil
}

// Remove deletes the state of one pin and its image when no other pin uses it.
// Removing an unknown or malformed ID is a no-op, and an already missing image
// file is ignored.
func (s *Store) Remove(id string) error {
	if _, err := validateID(id); err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return err
	}
	defer unlock()

	original := append([]State(nil), s.states...)
	next := make([]State, 0, len(s.states))
	var removed State
	found := false
	for _, current := range s.states {
		if current.ID == id {
			removed = current
			found = true
			continue
		}
		next = append(next, current)
	}
	if !found {
		return nil
	}
	if err := s.writeIndex(next); err != nil {
		return err
	}
	s.states = next
	for _, current := range next {
		if current.File == removed.File {
			return nil
		}
		// Distinct spellings can name the same file on macOS and Windows.
		if strings.EqualFold(current.File, removed.File) {
			removedInfo, removedErr := os.Stat(filepath.Join(s.dir, removed.File))
			currentInfo, currentErr := os.Stat(filepath.Join(s.dir, current.File))
			if removedErr == nil && currentErr == nil && os.SameFile(removedInfo, currentInfo) {
				return nil
			}
		}
	}
	if err := removeIfExists(filepath.Join(s.dir, removed.File)); err != nil {
		removeErr := fmt.Errorf("pin: remove image %q: %w", removed.File, err)
		if rollbackErr := s.writeIndex(original); rollbackErr != nil {
			return errors.Join(removeErr, fmt.Errorf("pin: restore state after failed image removal: %w", rollbackErr))
		}
		s.states = original
		return removeErr
	}
	return nil
}

// Clear removes every pin state and every pin image (including images orphaned
// by a corrupt index). The directory itself is kept.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return err
	}
	defer unlock()

	if err := s.writeIndex(nil); err != nil {
		return err
	}
	s.states = nil

	dirEntries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("pin: scan directory: %w", err)
	}
	var firstErr error
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}
		name := dirEntry.Name()
		if !isStaleTempFile(name) && !strings.HasSuffix(strings.ToLower(name), imageExt) {
			continue
		}
		if err := removeIfExists(filepath.Join(s.dir, name)); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("pin: remove %q: %w", name, err)
		}
	}
	return firstErr
}

// sanitizeStates validates the states loaded from pins.json: invalid IDs/files
// and entries whose image file no longer exists are dropped, values are
// normalized, duplicates are collapsed and the result is sorted. It reports
// whether the on-disk index needs to be rewritten.
func (s *Store) sanitizeStates(states []State) ([]State, bool) {
	original := append([]State(nil), states...)
	cleaned := make([]State, 0, len(states))
	seen := make(map[string]struct{}, len(states))
	for _, state := range states {
		normalized, err := normalizeState(state)
		if err != nil {
			continue
		}
		if _, duplicate := seen[normalized.ID]; duplicate {
			continue
		}
		data, err := diskio.ReadFile(filepath.Join(s.dir, normalized.File), imagebudget.MaxEncodedBytes)
		if err != nil {
			continue
		}
		if _, err := imagebudget.DecodePNG(data); err != nil {
			continue
		}
		seen[normalized.ID] = struct{}{}
		cleaned = append(cleaned, normalized)
	}
	sortStates(cleaned)
	return cleaned, !slices.Equal(cleaned, original)
}

// findLocked returns the stored state for id. The caller must hold s.mu.
func (s *Store) findLocked(id string) (State, bool) {
	for _, state := range s.states {
		if state.ID == id {
			return state, true
		}
	}
	return State{}, false
}

// writeIndex atomically replaces pins.json with states. Callers must hold
// s.mu, except during construction in Open.
func (s *Store) writeIndex(states []State) error {
	if states == nil {
		states = []State{}
	}
	payload := indexFile{Version: indexVersion, Pins: states}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("pin: encode index: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(filepath.Join(s.dir, indexFileName), data, 0o644); err != nil {
		if diskio.Committed(err) {
			s.states = states
		}
		return fmt.Errorf("pin: write index: %w", err)
	}
	return nil
}

// readIndex loads and decodes pins.json. A missing file is reported as
// os.ErrNotExist, undecodable content as errCorruptIndex.
func readIndex(dir string) ([]State, error) {
	data, err := diskio.ReadFile(filepath.Join(dir, indexFileName), maxIndexBytes)
	if err != nil {
		return nil, fmt.Errorf("pin: read index: %w", err)
	}
	// Check the version before decoding records: a newer schema may use a
	// different record shape and must never be mistaken for corrupt old data.
	var index struct {
		Version int             `json:"version"`
		Pins    json.RawMessage `json:"pins"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("%w: %v", errCorruptIndex, err)
	}
	if index.Version > indexVersion {
		return nil, fmt.Errorf("pin: index version %d is newer than supported version %d", index.Version, indexVersion)
	}
	var states []State
	if len(index.Pins) > 0 {
		if err := json.Unmarshal(index.Pins, &states); err != nil {
			return nil, fmt.Errorf("%w: %v", errCorruptIndex, err)
		}
	}
	return states, nil
}

// normalizeState validates and normalizes one state loaded from disk.
func normalizeState(st State) (State, error) {
	id, err := validateID(st.ID)
	if err != nil {
		return State{}, err
	}
	st.ID = id
	file := st.File
	if file == "" {
		file = id + imageExt
	}
	file, err = validateFile(file)
	if err != nil {
		return State{}, err
	}
	st.File = file
	st.Width = max(st.Width, minPinSize)
	st.Height = max(st.Height, minPinSize)
	st.Opacity = normalizeOpacity(st.Opacity)
	st.CreatedAt = st.CreatedAt.Round(0)
	return st, nil
}

// normalizeOpacity clamps a percentage to 10..100. Zero, negatives and NaN mean
// "unset" and default to fully opaque.
func normalizeOpacity(opacity float64) float64 {
	if math.IsNaN(opacity) || opacity <= 0 {
		return maxOpacity
	}
	if opacity < minOpacity {
		return minOpacity
	}
	if opacity > maxOpacity {
		return maxOpacity
	}
	return opacity
}

// validateID checks that id is safe to use as (part of) a file name.
func validateID(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidID)
	}
	if len(id) > maxIDLength {
		return "", fmt.Errorf("%w: longer than %d bytes", ErrInvalidID, maxIDLength)
	}
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("%w: %q contains unsafe characters", ErrInvalidID, id)
	}
	if isReservedWindowsName(id) {
		return "", fmt.Errorf("%w: %q is a reserved file name", ErrInvalidID, id)
	}
	return id, nil
}

// validateFile checks that file is a plain, relative .png file name.
func validateFile(file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidFile)
	}
	if len(file) > maxIDLength+len(imageExt) {
		return "", fmt.Errorf("%w: %q is too long", ErrInvalidFile, file)
	}
	if !filepath.IsLocal(file) || filepath.Base(file) != file {
		return "", fmt.Errorf("%w: %q is not a local file name", ErrInvalidFile, file)
	}
	if !filePattern.MatchString(file) {
		return "", fmt.Errorf("%w: %q contains unsafe characters", ErrInvalidFile, file)
	}
	if isReservedWindowsName(file) {
		return "", fmt.Errorf("%w: %q is a reserved file name", ErrInvalidFile, file)
	}
	return file, nil
}

// isReservedWindowsName reports whether name is a DOS device name, with or
// without an extension ("CON", "con.png" and "CON.txt.png" are all reserved).
func isReservedWindowsName(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	_, reserved := reservedWindowsNames[strings.ToUpper(base)]
	return reserved
}

// isStaleTempFile reports whether name is a temporary file created by
// writeFileAtomic that a crash left behind.
func isStaleTempFile(name string) bool {
	return strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-")
}

// removeStaleTempFiles best-effort deletes temporary files left behind by a
// crash during an atomic write.
func removeStaleTempFiles(dir string) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !isStaleTempFile(dirEntry.Name()) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, dirEntry.Name()))
	}
}

// sortStates orders states by CreatedAt ascending, then by ID. The order is
// stable across reloads and appends new pins at the end.
func sortStates(states []State) {
	sort.SliceStable(states, func(i, j int) bool {
		if !states[i].CreatedAt.Equal(states[j].CreatedAt) {
			return states[i].CreatedAt.Before(states[j].CreatedAt)
		}
		return states[i].ID < states[j].ID
	})
}

// writeFileAtomic writes data to path through a temporary file in the same
// directory followed by a rename, so readers never observe a partial file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	return diskio.WriteFileAtomic(path, data, mode)
}

// removeIfExists deletes path and treats an already missing file as success.
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
	current, err := readIndex(s.dir)
	if err != nil {
		unlock()
		return nil, err
	}
	s.states = current
	return unlock, nil
}

// PrepareRestore atomically archives older active pins beyond limit and returns
// the newest active pins. Archived records and their original images are kept.
func (s *Store) PrepareRestore(limit int) ([]State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockAndRefresh()
	if err != nil {
		return nil, err
	}
	defer unlock()
	next := append([]State(nil), s.states...)
	active := 0
	for _, st := range next {
		if !st.Archived {
			active++
		}
	}
	excess := max(0, active-max(limit, 0))
	changed := excess > 0
	for i := range next {
		if excess == 0 {
			break
		}
		if !next[i].Archived {
			next[i].Archived = true
			excess--
		}
	}
	if changed {
		if err := s.writeIndex(next); err != nil {
			return nil, err
		}
		s.states = next
	}
	result := make([]State, 0, len(next))
	for _, st := range next {
		if !st.Archived {
			result = append(result, st)
		}
	}
	return result, nil
}
