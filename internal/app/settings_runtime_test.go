package app

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/echoVic/Pinstill/internal/store"
)

// These callbacks model registration ownership without installing native
// shortcuts or invoking their capture/pin handlers.
type runtimeTestNative struct {
	registered map[string]bool
	fail       map[string]error
	changes    []string
	login      bool
	loginErr   error
	loginSets  []bool
}

func (n *runtimeTestNative) callbacks() settingsRuntime {
	return settingsRuntime{
		register: func(acc string, _ func()) error {
			n.changes = append(n.changes, "register "+acc)
			if err := n.fail[acc]; err != nil {
				return err
			}
			if n.registered[acc] {
				return errors.New("accelerator already registered")
			}
			n.registered[acc] = true
			return nil
		},
		unregister: func(acc string) {
			n.changes = append(n.changes, "unregister "+acc)
			delete(n.registered, acc)
		},
		getLogin: func() bool { return n.login },
		setLogin: func(enabled bool) error {
			n.loginSets = append(n.loginSets, enabled)
			if n.loginErr != nil {
				return n.loginErr
			}
			n.login = enabled
			return nil
		},
	}
}

func runtimeTestApp() (*App, *runtimeTestNative) {
	s := DefaultSettings()
	s.CaptureKey, s.PinKey, s.OcrKey = "capture-old", "pin-old", ""
	a := &App{settings: s, shortcutKeys: map[string]string{
		"captureKey": s.CaptureKey, "pinKey": s.PinKey,
	}}
	n := &runtimeTestNative{registered: map[string]bool{s.CaptureKey: true, s.PinKey: true}, fail: map[string]error{}}
	return a, n
}

func TestEffectiveSettingsRetainWorkingShortcuts(t *testing.T) {
	a, n := runtimeTestApp()
	s := a.settingsSnapshot()
	s.Format = "JPEG"
	// A re-registration would fail, even though the existing registration
	// continues to work. Changing an unrelated setting must leave it alone.
	n.fail[s.CaptureKey] = errors.New("registration unavailable")
	if errs := a.applyEffectiveSettingsWith(s, n.callbacks()); len(errs) != 0 {
		t.Fatalf("unrelated setting failed: %v", errs)
	}
	if len(n.changes) != 0 || len(n.registered) != 2 || a.settingsSnapshot().Format != "JPEG" {
		t.Fatalf("unrelated setting disturbed shortcuts: changes=%v active=%v settings=%+v", n.changes, n.registered, a.settingsSnapshot())
	}
}

func TestEffectiveSettingsShortcutRollback(t *testing.T) {
	for _, restoreFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "restore failure explicit"}[restoreFails], func(t *testing.T) {
			a, n := runtimeTestApp()
			s := a.settingsSnapshot()
			s.CaptureKey, s.PinKey, s.Format = "capture-new", "pin-new", "JPEG"
			n.fail[s.CaptureKey] = errors.New("new accelerator busy")
			if restoreFails {
				n.fail["capture-old"] = errors.New("old accelerator also busy")
			}
			errs := a.applyEffectiveSettingsWith(s, n.callbacks())
			if len(errs) != 1 || !strings.Contains(errs["captureKey"], "new accelerator busy") {
				t.Fatalf("shortcut errors = %v", errs)
			}
			wantCapture := "capture-old"
			if restoreFails {
				wantCapture = ""
				if !strings.Contains(errs["captureKey"], "old accelerator also busy") {
					t.Errorf("rollback failure hidden: %v", errs)
				}
			}
			got := a.settingsSnapshot()
			if got.CaptureKey != wantCapture || got.PinKey != "pin-new" || got.Format != "JPEG" {
				t.Fatalf("effective settings lost rollback or independent success: %+v", got)
			}
			if a.shortcutKeys["captureKey"] != wantCapture || !n.registered["pin-new"] || n.registered["capture-new"] || (wantCapture != "" && !n.registered[wantCapture]) {
				t.Fatalf("effective shortcut state disagrees: keys=%v registered=%v", a.shortcutKeys, n.registered)
			}
		})
	}
}

func TestEffectiveSettingsShortcutSwapAndRollback(t *testing.T) {
	for _, failSwap := range []bool{false, true} {
		t.Run(map[bool]string{false: "swap", true: "dependent rollback"}[failSwap], func(t *testing.T) {
			a, n := runtimeTestApp()
			s := a.settingsSnapshot()
			s.CaptureKey, s.PinKey = "pin-old", "capture-old"
			ops := n.callbacks()
			register := ops.register
			failed := false
			ops.register = func(acc string, fn func()) error {
				if failSwap && acc == "pin-old" && !failed {
					failed = true
					return errors.New("swap registration failed")
				}
				return register(acc, fn)
			}
			errs := a.applyEffectiveSettingsWith(s, ops)
			got := a.settingsSnapshot()
			if failSwap {
				if errs["captureKey"] == "" || errs["pinKey"] == "" || got.CaptureKey != "capture-old" || got.PinKey != "pin-old" {
					t.Fatalf("dependent rollback = %+v, errors=%v", got, errs)
				}
			} else if len(errs) != 0 || got.CaptureKey != "pin-old" || got.PinKey != "capture-old" {
				t.Fatalf("shortcut swap = %+v, errors=%v", got, errs)
			}
			if len(n.registered) != 2 || !n.registered[got.CaptureKey] || !n.registered[got.PinKey] {
				t.Fatalf("shortcut ownership lost: %v", n.registered)
			}
		})
	}
}

func TestEffectiveSettingsDisableShortcutAndOCR(t *testing.T) {
	a, n := runtimeTestApp()
	a.shortcutKeys["ocrKey"] = "legacy-ocr"
	n.registered["legacy-ocr"] = true
	s := a.settingsSnapshot()
	s.CaptureKey, s.OcrKey = "", "ocr-requested"
	errs := a.applyEffectiveSettingsWith(s, n.callbacks())
	if len(errs) != 1 || errs["ocrKey"] == "" {
		t.Fatalf("unsupported OCR must be explicit: %v", errs)
	}
	got := a.settingsSnapshot()
	if got.CaptureKey != "" || got.OcrKey != "" || !reflect.DeepEqual(n.registered, map[string]bool{"pin-old": true}) {
		t.Fatalf("disabled/placeholder shortcuts remain active: settings=%+v active=%v", got, n.registered)
	}
	for _, change := range n.changes {
		if change == "register ocr-requested" {
			t.Fatal("registered an OCR no-op placeholder")
		}
	}
}

func TestEffectiveSettingsLoginFailure(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disable", true: "enable"}[enabled], func(t *testing.T) {
			a, n := runtimeTestApp()
			n.login, n.loginErr = !enabled, errors.New("login item denied")
			s := a.settingsSnapshot()
			s.LaunchAtLogin, s.Format = enabled, "JPEG"
			errs := a.applyEffectiveSettingsWith(s, n.callbacks())
			if len(errs) != 1 || !strings.Contains(errs["launchAtLogin"], "login item denied") {
				t.Fatalf("login failure hidden: %v", errs)
			}
			got := a.settingsSnapshot()
			if got.LaunchAtLogin != !enabled || got.Format != "JPEG" || !reflect.DeepEqual(n.loginSets, []bool{enabled}) {
				t.Fatalf("failed requested login state published: %+v, attempts=%v", got, n.loginSets)
			}
		})
	}
}

func TestEffectiveSettingsLoginSuccess(t *testing.T) {
	a, n := runtimeTestApp()
	for _, enabled := range []bool{true, true, false} {
		s := a.settingsSnapshot()
		s.LaunchAtLogin = enabled
		if errs := a.applyEffectiveSettingsWith(s, n.callbacks()); len(errs) != 0 || a.settingsSnapshot().LaunchAtLogin != enabled || n.login != enabled {
			t.Fatalf("login transition failed: errors=%v state=%v", errs, n.login)
		}
	}
	if !reflect.DeepEqual(n.loginSets, []bool{true, false}) {
		t.Fatalf("unchanged login state rewritten: %v", n.loginSets)
	}
}

func runtimeTestHistory(t *testing.T, a *App, blocked bool) store.Entry {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "history"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := st.Save(gnPNGData(t, 4, 3), time.Now().Add(-10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	a.store = st
	if blocked {
		index := filepath.Join(st.Dir(), "index.json")
		if err := os.Rename(index, index+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(index, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(index, "blocker"), []byte("block index replacement"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return entry
}

func TestEffectiveSettingsHistoryClearFailure(t *testing.T) {
	a, n := runtimeTestApp()
	entry := runtimeTestHistory(t, a, true)
	s := a.settingsSnapshot()
	s.KeepHistory, s.Format = false, "JPEG"
	errs := a.applyEffectiveSettingsWith(s, n.callbacks())
	if len(errs) != 1 || errs["keepHistory"] == "" {
		t.Fatalf("history clear failure hidden: %v", errs)
	}
	if got := a.settingsSnapshot(); !got.KeepHistory || got.Format != "JPEG" {
		t.Fatalf("failed history disable published: %+v", got)
	}
	if entries := a.store.List(); len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("failed clear lost history: %v", entries)
	}
}

func TestEffectiveSettingsHistoryPruneFailure(t *testing.T) {
	a, n := runtimeTestApp()
	entry := runtimeTestHistory(t, a, true)
	old := a.settingsSnapshot()
	s := old
	s.RetentionDays, s.HistoryLimitMB, s.Format = 1, 1, "JPEG"
	errs := a.applyEffectiveSettingsWith(s, n.callbacks())
	if len(errs) != 2 || errs["retentionDays"] == "" || errs["historyLimitMB"] == "" {
		t.Fatalf("history prune failure hidden: %v", errs)
	}
	got := a.settingsSnapshot()
	if got.RetentionDays != old.RetentionDays || got.HistoryLimitMB != old.HistoryLimitMB || got.Format != "JPEG" {
		t.Fatalf("failed prune settings published: %+v", got)
	}
	if entries := a.store.List(); len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("failed prune lost history: %v", entries)
	}
}

func TestEffectiveSettingsHistorySuccess(t *testing.T) {
	for _, keep := range []bool{true, false} {
		t.Run(map[bool]string{true: "requested retention prunes", false: "disable clears"}[keep], func(t *testing.T) {
			a, n := runtimeTestApp()
			runtimeTestHistory(t, a, false)
			s := a.settingsSnapshot()
			s.KeepHistory, s.RetentionDays = keep, 1
			if errs := a.applyEffectiveSettingsWith(s, n.callbacks()); len(errs) != 0 {
				t.Fatalf("history change failed: %v", errs)
			}
			if len(a.store.List()) != 0 || a.settingsSnapshot().KeepHistory != keep || a.settingsSnapshot().RetentionDays != 1 {
				t.Fatalf("requested history operation not applied: %+v", a.settingsSnapshot())
			}
		})
	}
}

func TestEffectiveSettingsPublishAfterOperations(t *testing.T) {
	a, n := runtimeTestApp()
	old := a.settingsSnapshot()
	s := old
	s.LaunchAtLogin, s.Format = true, "JPEG"
	ops := n.callbacks()
	setLogin := ops.setLogin
	ops.setLogin = func(enabled bool) error {
		if got := a.settingsSnapshot(); !reflect.DeepEqual(got, old) {
			t.Errorf("requested settings visible before operation finished: %+v", got)
		}
		return setLogin(enabled)
	}
	if errs := a.applyEffectiveSettingsWith(s, ops); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := a.settingsSnapshot(); !reflect.DeepEqual(got, s) {
		t.Fatalf("successful settings not published: %+v", got)
	}
}

func TestEffectiveSettingsSerializesOperations(t *testing.T) {
	a, n := runtimeTestApp()
	s := a.settingsSnapshot()
	s.LaunchAtLogin = true
	entered, release := make(chan struct{}), make(chan struct{})
	ops := n.callbacks()
	setLogin := ops.setLogin
	var once sync.Once
	ops.setLogin = func(enabled bool) error {
		once.Do(func() { close(entered); <-release })
		return setLogin(enabled)
	}
	done := make(chan struct{})
	go func() { a.applyEffectiveSettingsWith(s, ops); close(done) }()
	<-entered
	locked := a.settingsMu.TryLock()
	if locked {
		a.settingsMu.Unlock()
	}
	close(release)
	<-done
	if locked {
		t.Fatal("native operations ran without settingsMu")
	}
}

func TestHistoryByteLimit(t *testing.T) {
	for _, tt := range []struct {
		mb   int
		want int64
	}{{-1, 0}, {0, 0}, {1, 1048576}, {2048, 2147483648}} {
		if got := historyByteLimit(tt.mb); got != tt.want {
			t.Errorf("historyByteLimit(%d) = %d, want %d", tt.mb, got, tt.want)
		}
	}
	// Values from an oversized preference must not wrap to a negative limit
	// and silently disable size pruning.
	if large := int64(math.MaxInt64); int64(int(large)) == large {
		if got := historyByteLimit(int(large)); got != math.MaxInt64 {
			t.Errorf("oversized history byte limit = %d, want saturation", got)
		}
	}
}
