package app

import (
	"errors"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/store"
	"github.com/egoist/mygo"
)

func TestClampToDisplay(t *testing.T) {
	tests := []struct {
		name   string
		rect   capture.Rect
		bounds mygo.Rectangle
		want   capture.Rect
	}{
		{
			name:   "inside is unchanged",
			rect:   capture.Rect{X: 100, Y: 100, Width: 200, Height: 150},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080},
			want:   capture.Rect{X: 100, Y: 100, Width: 200, Height: 150},
		},
		{
			name:   "exactly the display bounds is unchanged",
			rect:   capture.Rect{X: 0, Y: 0, Width: 100, Height: 100},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{X: 0, Y: 0, Width: 100, Height: 100},
		},
		{
			name:   "partially outside top left is clipped",
			rect:   capture.Rect{X: 80, Y: 30, Width: 200, Height: 100},
			bounds: mygo.Rectangle{X: 100, Y: 50, Width: 1000, Height: 800},
			want:   capture.Rect{X: 100, Y: 50, Width: 180, Height: 80},
		},
		{
			name:   "partially outside bottom right is clipped",
			rect:   capture.Rect{X: 900, Y: 700, Width: 300, Height: 200},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1000, Height: 800},
			want:   capture.Rect{X: 900, Y: 700, Width: 100, Height: 100},
		},
		{
			name:   "fully outside right is empty",
			rect:   capture.Rect{X: 200, Y: 0, Width: 50, Height: 50},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{},
		},
		{
			name:   "fully outside left is empty",
			rect:   capture.Rect{X: -100, Y: 0, Width: 50, Height: 50},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{},
		},
		{
			name:   "touching the edge is empty",
			rect:   capture.Rect{X: 100, Y: 0, Width: 50, Height: 50},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{},
		},
		{
			name:   "negative display origin clips the left edge",
			rect:   capture.Rect{X: -2000, Y: -100, Width: 300, Height: 200},
			bounds: mygo.Rectangle{X: -1920, Y: 0, Width: 1920, Height: 1080},
			want:   capture.Rect{X: -1920, Y: 0, Width: 220, Height: 100},
		},
		{
			name:   "selection on a negative-origin display is unchanged",
			rect:   capture.Rect{X: -1000, Y: 100, Width: 200, Height: 150},
			bounds: mygo.Rectangle{X: -1920, Y: 0, Width: 1920, Height: 1080},
			want:   capture.Rect{X: -1000, Y: 100, Width: 200, Height: 150},
		},
		{
			name:   "zero-size selection is empty",
			rect:   capture.Rect{X: 10, Y: 10},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{},
		},
		{
			name:   "negative size selection is empty",
			rect:   capture.Rect{X: 50, Y: 50, Width: -10, Height: -10},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{},
		},
		{
			name:   "fractional selection is unchanged",
			rect:   capture.Rect{X: 10.5, Y: 10.25, Width: 20.5, Height: 30.75},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{X: 10.5, Y: 10.25, Width: 20.5, Height: 30.75},
		},
		{
			name:   "selection larger than the display is clamped to it",
			rect:   capture.Rect{X: -50, Y: -50, Width: 300, Height: 300},
			bounds: mygo.Rectangle{X: 0, Y: 0, Width: 100, Height: 100},
			want:   capture.Rect{X: 0, Y: 0, Width: 100, Height: 100},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampToDisplay(tt.rect, tt.bounds); got != tt.want {
				t.Fatalf("clampToDisplay(%+v, %+v) = %+v, want %+v", tt.rect, tt.bounds, got, tt.want)
			}
		})
	}
}

func TestShotURL(t *testing.T) {
	mac := &App{shotBase: "shot://localhost"}
	windows := &App{shotBase: "http://shot.localhost"}
	empty := &App{}

	tests := []struct {
		name string
		app  *App
		path string
		want string
	}{
		{name: "empty path", app: mac, path: "", want: ""},
		{name: "relative path", app: mac, path: "h/shot.png", want: "shot://localhost/h/shot.png"},
		{name: "leading slash", app: mac, path: "/h/shot.png", want: "shot://localhost/h/shot.png"},
		{name: "session file", app: mac, path: "/s/pin-1.png", want: "shot://localhost/s/pin-1.png"},
		{name: "windows base", app: windows, path: "/h/shot.png", want: "http://shot.localhost/h/shot.png"},
		{name: "empty base", app: empty, path: "h/shot.png", want: "/h/shot.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.shotURL(tt.path); got != tt.want {
				t.Fatalf("shotURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// newTestApp returns an App with a real store on a temporary directory and no
// windows, which is enough for the history helpers that do not touch native UI.
func newTestApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	return &App{
		store:    st,
		shotBase: "shot://localhost",
		settings: DefaultSettings(),
	}
}

func TestHistoryEntries(t *testing.T) {
	a := newTestApp(t)
	createdAt := time.Date(2026, 11, 9, 10, 30, 0, 0, time.UTC)
	older, err := a.store.Save(gnPNGData(t, 40, 30), createdAt)
	if err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}
	newer, err := a.store.Save(gnPNGData(t, 20, 10), createdAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	entries := a.historyEntries()
	if len(entries) != 2 {
		t.Fatalf("historyEntries() returned %d entries, want 2: %+v", len(entries), entries)
	}
	want := []HistoryEntry{
		{
			ID:        newer.ID,
			CreatedAt: newer.CreatedAt.Format(time.RFC3339),
			Width:     newer.Width,
			Height:    newer.Height,
			Size:      newer.Size,
			URL:       "shot://localhost/h/" + newer.File,
			ThumbURL:  "shot://localhost/h/" + newer.Thumb,
		},
		{
			ID:        older.ID,
			CreatedAt: older.CreatedAt.Format(time.RFC3339),
			Width:     older.Width,
			Height:    older.Height,
			Size:      older.Size,
			URL:       "shot://localhost/h/" + older.File,
			ThumbURL:  "shot://localhost/h/" + older.Thumb,
		},
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Fatalf("historyEntries()[%d] = %+v, want %+v", i, entries[i], want[i])
		}
	}

	// With history disabled the list is always empty, without consulting the store.
	a.settings.KeepHistory = false
	if entries := a.historyEntries(); len(entries) != 0 {
		t.Fatalf("historyEntries() with KeepHistory=false = %+v, want empty", entries)
	}

	// Without storage the app reports an empty history too.
	a.settings.KeepHistory = true
	a.store = nil
	if entries := a.historyEntries(); len(entries) != 0 {
		t.Fatalf("historyEntries() without store = %+v, want empty", entries)
	}
}

func TestHistoryCapture(t *testing.T) {
	a := newTestApp(t)
	entry, err := a.store.Save(gnPNGData(t, 64, 48), time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	got, err := a.historyCapture(entry.ID)
	if err != nil {
		t.Fatalf("historyCapture(%q) error = %v", entry.ID, err)
	}
	want := HistoryCapture{
		ID:     entry.ID,
		Width:  entry.Width,
		Height: entry.Height,
		URL:    "shot://localhost/h/" + entry.File,
	}
	if got != want {
		t.Fatalf("historyCapture() = %+v, want %+v", got, want)
	}
	if _, err := a.historyCapture("20990101-000000-aaaa"); err == nil {
		t.Fatal("historyCapture(unknown) returned nil error")
	}

	noStore := &App{}
	if _, err := noStore.historyCapture(entry.ID); err == nil {
		t.Fatal("historyCapture() without store returned nil error")
	}
}

func TestRemoveAndClearHistory(t *testing.T) {
	a := newTestApp(t)
	first, err := a.store.Save(gnPNGData(t, 32, 32), time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}
	if _, err := a.store.Save(gnPNGData(t, 32, 32), time.Date(2026, 11, 9, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	if err := a.removeHistory(first.ID); err != nil {
		t.Fatalf("removeHistory() error = %v", err)
	}
	if _, ok := a.store.Get(first.ID); ok {
		t.Fatalf("entry %q still present after removeHistory", first.ID)
	}
	if entries := a.historyEntries(); len(entries) != 1 {
		t.Fatalf("historyEntries() after removeHistory = %d entries, want 1", len(entries))
	}
	// Removing an unknown ID is a no-op, mirroring store.Delete.
	if err := a.removeHistory("20990101-000000-aaaa"); err != nil {
		t.Fatalf("removeHistory(unknown) error = %v", err)
	}

	if err := a.clearHistory(); err != nil {
		t.Fatalf("clearHistory() error = %v", err)
	}
	if entries := a.historyEntries(); len(entries) != 0 {
		t.Fatalf("historyEntries() after clearHistory = %+v, want empty", entries)
	}

	noStore := &App{}
	if err := noStore.removeHistory("20990101-000000-aaaa"); err == nil {
		t.Fatal("removeHistory() without store returned nil error")
	}
	if err := noStore.clearHistory(); err == nil {
		t.Fatal("clearHistory() without store returned nil error")
	}
}

func TestCopyImagePNGRejectsInvalidData(t *testing.T) {
	// The invalid path must fail before the system clipboard is touched, so it
	// is safe to exercise headless.
	a := &App{}
	if err := a.copyImagePNG([]byte("definitely not a png")); err == nil {
		t.Fatal("copyImagePNG(garbage) returned nil error")
	}
}

func TestStoreCaptureEnforcesHistoryRetention(t *testing.T) {
	a := newTestApp(t)
	a.sessionDir = t.TempDir()
	a.settings.RetentionDays = 7
	old, err := a.store.Save(gnPNGData(t, 32, 32), time.Now().AddDate(0, 0, -8))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.storeCapture(gnPNGData(t, 24, 24)); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.store.Get(old.ID); ok {
		t.Fatal("new capture retained an expired history entry")
	}
	if entries := a.store.List(); len(entries) != 1 || entries[0].Width != 24 {
		t.Fatalf("history should contain only the new capture: %+v", entries)
	}
}

func TestFailedSelectionCanBeRetried(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	a := newTestApp(t)
	a.sessionDir = t.TempDir()
	a.capturing = true
	a.overlayDisplay = mygo.Display{Bounds: mygo.Rectangle{Width: 1920, Height: 1080}}
	a.failSelection(errors.New("temporary capture failure"), false)
	if _, err := a.prepareSelectionWithDelay(Rect{Width: 100, Height: 100}, 0); err != nil {
		t.Fatalf("retry in the still-open overlay failed: %v", err)
	}
}

func TestCancelledPreparationDoesNotWriteIntoNextCapture(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	for _, restart := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			a := newTestApp(t)
			a.sessionDir = t.TempDir()
			a.capturing = true
			a.overlayDisplay = mygo.Display{Bounds: mygo.Rectangle{Width: 1920, Height: 1080}}
			done := make(chan error, 1)
			go func() {
				_, err := a.prepareSelectionWithDelay(Rect{Width: 100, Height: 100}, time.Second)
				done <- err
			}()
			synctest.Wait() // Request is waiting for the compositor delay.
			a.cancelRegion()
			if restart {
				// Start a new session without opening native windows.
				a.mu.Lock()
				a.capturing = true
				a.mu.Unlock()
			}
			time.Sleep(2 * time.Second)
			if err := <-done; err == nil {
				t.Error("cancelled preparation returned a usable capture")
			}
			files, err := os.ReadDir(a.sessionDir)
			if err != nil || len(files) != 0 {
				t.Fatalf("cancelled preparation left session images: %v (%v)", files, err)
			}
		})
	}
}

func TestSelfTestsUseSeparatePageStorage(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST", "history")
	a := New()
	if a.pageSession == nil || !a.pageSession.IsEphemeral() {
		t.Fatal("selftest must not share the default persistent WebView store")
	}
	t.Setenv("PINSTILL_SELFTEST", "")
	if a := New(); a.pageSession != nil {
		t.Fatal("normal launch must continue using the existing persistent WebView store")
	}
}

func TestStoreCaptureIdentifiesItsOwnHistoryEntry(t *testing.T) {
	a := newTestApp(t)
	a.sessionDir = t.TempDir()
	for _, keep := range []bool{true, false} {
		a.settings.KeepHistory = keep
		payload, err := a.storeCapture(gnPNGData(t, 24, 16))
		if err != nil {
			t.Fatal(err)
		}
		if keep {
			entry, ok := a.store.Get(payload.HistoryID)
			if !ok || entry.Width != payload.Width || entry.Height != payload.Height {
				t.Fatalf("capture does not identify the history image it saved: %+v", payload)
			}
		} else if payload.HistoryID != "" {
			t.Fatal("capture with history disabled must not identify an older history image")
		}
	}
}
