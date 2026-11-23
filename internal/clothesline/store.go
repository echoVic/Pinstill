package clothesline

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/echoVic/Pinstill/internal/diskio"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"golang.org/x/image/draw"
)

const (
	indexVersion = 1
	indexFile    = "line.json"
	maxItems     = 60
	maxBytes     = 512 << 20
	previewEdge  = 320
)

var itemID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int    `json:"size"`
}

type State struct {
	Version  int    `json:"version"`
	Revision uint64 `json:"revision"`
	Visible  bool   `json:"visible"`
	Items    []Item `json:"items"`
}

type Store struct{ dir string }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (line *Store) Dir() string { return line.dir }

func (line *Store) Snapshot() (State, error) {
	unlock, err := diskio.LockDir(line.dir)
	if err != nil {
		return State{}, err
	}
	defer unlock()
	return line.readState()
}

func (line *Store) readState() (State, error) {
	state := State{Version: indexVersion, Items: []Item{}}
	data, err := diskio.ReadFile(filepath.Join(line.dir, indexFile), 256<<10)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return State{}, err
	}
	state.Version = 0
	if err := json.Unmarshal(data, &state); err != nil || state.Version != indexVersion || len(state.Items) > maxItems {
		return State{}, errors.New("无法读取晾绳记录")
	}
	seen := make(map[string]bool, len(state.Items))
	for _, item := range state.Items {
		if !itemID.MatchString(item.ID) || seen[item.ID] || item.Size <= 0 || item.Size > imagebudget.MaxEncodedBytes {
			return State{}, errors.New("晾绳记录包含无效图片")
		}
		seen[item.ID] = true
	}
	return state, nil
}

func (line *Store) writeState(state State) (State, error) {
	state.Revision++
	data, err := json.Marshal(state)
	if err != nil {
		return State{}, err
	}
	err = diskio.WriteFileAtomic(filepath.Join(line.dir, indexFile), data, 0o600)
	if err != nil && !diskio.Committed(err) {
		return State{}, err
	}
	return state, err
}

func (line *Store) Add(data []byte, name string) (State, error) {
	unlock, err := diskio.LockDir(line.dir)
	if err != nil {
		return State{}, err
	}
	defer unlock()
	state, err := line.readState()
	if err != nil {
		return State{}, err
	}
	if len(state.Items) >= maxItems {
		return State{}, errors.New("晾绳已挂满 60 张图片，请取下一张再添加")
	}
	img, format, err := imagebudget.Decode(data)
	if err != nil || (format != "jpeg" && format != "png") {
		return State{}, errors.New("请选择有效的 PNG 或 JPEG 图片，单张不超过 64 MiB 和 32 Mi 像素")
	}
	if format != "png" {
		data, err = imagebudget.EncodePNG(img)
		if err != nil {
			return State{}, err
		}
	}
	remaining := int64(maxBytes)
	for _, item := range state.Items {
		remaining -= int64(item.Size)
	}
	if int64(len(data)) > remaining {
		return State{}, errors.New("晾绳的图片已接近 512 MiB，请取下一些图片再添加")
	}
	preview, err := encodePreview(img)
	if err != nil {
		return State{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return State{}, err
	}
	id := hex.EncodeToString(random[:])
	imagePath := filepath.Join(line.dir, id+".png")
	previewPath := filepath.Join(line.dir, id+".thumb.png")
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(imagePath)
			_ = os.Remove(previewPath)
		}
	}()
	if err := diskio.WriteFileAtomic(imagePath, data, 0o600); err != nil {
		return State{}, err
	}
	if err := diskio.WriteFileAtomic(previewPath, preview, 0o600); err != nil {
		return State{}, err
	}
	title := []rune(strings.TrimSpace(name))
	if len(title) == 0 {
		title = []rune("截图")
	}
	state.Items = append(state.Items, Item{ID: id, Name: string(title[:min(len(title), 80)]), Size: len(data)})
	state.Visible = true
	stored, err := line.writeState(state)
	committed = err == nil || diskio.Committed(err)
	return stored, err
}

func encodePreview(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	scale := min(1, previewEdge/float64(max(bounds.Dx(), bounds.Dy())))
	width := max(1, int(math.Round(float64(bounds.Dx())*scale)))
	height := max(1, int(math.Round(float64(bounds.Dy())*scale)))
	preview := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(preview, preview.Bounds(), img, bounds, draw.Src, nil)
	return imagebudget.EncodePNG(preview)
}

func (line *Store) SetVisible(visible bool) (State, error) {
	return line.change(func(state *State) (bool, error) {
		changed := state.Visible != visible
		state.Visible = visible
		return changed, nil
	})
}

func (line *Store) ToggleVisible() (State, error) {
	return line.change(func(state *State) (bool, error) {
		state.Visible = !state.Visible
		return true, nil
	})
}

func (line *Store) Move(id, before string) (State, error) {
	return line.change(func(state *State) (bool, error) {
		from, to := itemIndex(state.Items, id), len(state.Items)
		if before != "" {
			to = itemIndex(state.Items, before)
		}
		if from < 0 || to < 0 {
			return false, errors.New("该图片已取下，请重新选择")
		}
		if from < to {
			to--
		}
		if from == to {
			return false, nil
		}
		moving := state.Items[from]
		state.Items = slices.Insert(slices.Delete(state.Items, from, from+1), to, moving)
		return true, nil
	})
}

func (line *Store) Remove(id string) (State, error) {
	removed := false
	state, err := line.change(func(state *State) (bool, error) {
		index := itemIndex(state.Items, id)
		if index < 0 {
			return false, nil
		}
		state.Items = slices.Delete(state.Items, index, index+1)
		removed = true
		return true, nil
	})
	if err != nil || !removed {
		return state, err
	}
	var cleanupError error
	for _, suffix := range []string{".png", ".thumb.png"} {
		if err := os.Remove(filepath.Join(line.dir, id+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupError = errors.Join(cleanupError, err)
		}
	}
	if cleanupError != nil {
		return state, diskio.MarkCommitted(cleanupError)
	}
	return state, nil
}

func (line *Store) Read(id string) ([]byte, error) {
	unlock, err := diskio.LockDir(line.dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	state, err := line.readState()
	if err != nil {
		return nil, err
	}
	if itemIndex(state.Items, id) < 0 {
		return nil, errors.New("该图片已取下")
	}
	return diskio.ReadFile(filepath.Join(line.dir, id+".png"), imagebudget.MaxEncodedBytes)
}

func (line *Store) change(apply func(*State) (bool, error)) (State, error) {
	unlock, err := diskio.LockDir(line.dir)
	if err != nil {
		return State{}, err
	}
	defer unlock()
	state, err := line.readState()
	if err != nil {
		return State{}, err
	}
	changed, err := apply(&state)
	if err != nil || !changed {
		return state, err
	}
	return line.writeState(state)
}

func itemIndex(items []Item, id string) int {
	return slices.IndexFunc(items, func(item Item) bool { return item.ID == id })
}
