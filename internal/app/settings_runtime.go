package app

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/egoist/mygo"
)

type settingsRuntime struct {
	register   func(string, func()) error
	unregister func(string)
	getLogin   func() bool
	setLogin   func(bool) error
}

func historyByteLimit(megabytes int) int64 {
	if megabytes <= 0 {
		return 0
	}
	if int64(megabytes) > math.MaxInt64/(1024*1024) {
		return math.MaxInt64
	}
	return int64(megabytes) * 1024 * 1024
}

func nativeSettingsRuntime() settingsRuntime {
	r := settingsRuntime{
		register: mygo.GlobalShortcut.Register, unregister: mygo.GlobalShortcut.Unregister,
	}
	if os.Getenv("PINSTILL_SELFTEST") == "" {
		r.getLogin, r.setLogin = mygo.App.OpenAtLogin, mygo.App.SetOpenAtLogin
	}
	return r
}

// reconcileShortcuts is the startup entry point. applyEffectiveSettings uses
// the locked helper so settingsMu is never acquired twice in one operation.
func (a *App) reconcileShortcuts(s Settings) map[string]string {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	return a.reconcileShortcutsWith(s, nativeSettingsRuntime())
}

func (a *App) shortcutHandlers() map[string]func() {
	return map[string]func(){
		"captureKey": func() {
			a.mu.Lock()
			a.hotkeysFired++
			a.mu.Unlock()
			go a.beginRegion()
		},
		"pinKey": func() {
			go func() {
				if _, err := a.pinClipboard(); err != nil {
					a.toast(err.Error(), "error")
				}
			}()
		},
	}
}

// reconcileShortcutsWith requires settingsMu. Only changed registrations are
// released; swaps are applied together so a failure can restore both owners.
func (a *App) reconcileShortcutsWith(s Settings, r settingsRuntime) map[string]string {
	a.mu.Lock()
	active := make(map[string]string, len(a.shortcutKeys))
	for key, acc := range a.shortcutKeys {
		active[key] = acc
	}
	a.mu.Unlock()
	errs := map[string]string{}
	if acc := active["ocrKey"]; acc != "" {
		r.unregister(acc)
	}
	delete(active, "ocrKey")
	if strings.TrimSpace(s.OcrKey) != "" {
		errs["ocrKey"] = "提取文字可从截图工具栏使用，未注册全局快捷键"
	}
	want := map[string]string{
		"captureKey": strings.TrimSpace(s.CaptureKey),
		"pinKey":     strings.TrimSpace(s.PinKey),
	}
	handlers := a.shortcutHandlers()
	var groups [][]string
	for _, key := range []string{"captureKey", "pinKey"} {
		if want[key] != active[key] {
			groups = append(groups, []string{key})
		}
	}
	if len(groups) == 2 && ((want["captureKey"] != "" && want["captureKey"] == active["pinKey"]) ||
		(want["pinKey"] != "" && want["pinKey"] == active["captureKey"])) {
		groups = [][]string{{"captureKey", "pinKey"}}
	}
	for _, group := range groups {
		old := make(map[string]string, len(group))
		for _, key := range group {
			old[key] = active[key]
			if active[key] != "" {
				r.unregister(active[key])
			}
			delete(active, key)
		}
		failed := false
		for _, key := range group {
			if want[key] == "" {
				continue
			}
			if err := r.register(want[key], handlers[key]); err != nil {
				errs[key] = err.Error()
				failed = true
				break
			}
			active[key] = want[key]
		}
		if failed {
			// Release any successfully moved keys before restoring the group.
			for _, key := range group {
				if acc := active[key]; acc != "" {
					r.unregister(acc)
				}
				delete(active, key)
				if errs[key] == "" {
					errs[key] = "关联快捷键修改失败，本次修改已撤回"
				}
			}
			for _, key := range group {
				if old[key] == "" {
					continue
				}
				if err := r.register(old[key], handlers[key]); err != nil {
					errs[key] += fmt.Sprintf("；恢复原快捷键失败: %v", err)
					continue
				}
				active[key] = old[key]
			}
		}
	}
	a.mu.Lock()
	a.shortcutKeys = active
	a.mu.Unlock()
	return errs
}

func (a *App) applyEffectiveSettings(s Settings) map[string]string {
	return a.applyEffectiveSettingsWith(s, nativeSettingsRuntime())
}

func (a *App) applyEffectiveSettingsWith(s Settings, r settingsRuntime) map[string]string {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	previous := a.settingsSnapshot()
	effective := s
	errs := a.reconcileShortcutsWith(s, r)
	a.mu.Lock()
	effective.CaptureKey = a.shortcutKeys["captureKey"]
	effective.PinKey = a.shortcutKeys["pinKey"]
	effective.OcrKey = ""
	a.mu.Unlock()
	if r.getLogin != nil {
		effective.LaunchAtLogin = r.getLogin()
		if effective.LaunchAtLogin != s.LaunchAtLogin {
			if err := r.setLogin(s.LaunchAtLogin); err != nil {
				errs["launchAtLogin"] = err.Error()
			} else {
				effective.LaunchAtLogin = s.LaunchAtLogin
			}
		}
	}
	historyChanged := false
	if a.store != nil {
		if s.KeepHistory {
			removed, err := a.store.Prune(s.RetentionDays, historyByteLimit(s.HistoryLimitMB))
			historyChanged = removed > 0
			if err != nil {
				errs["retentionDays"] = err.Error()
				errs["historyLimitMB"] = err.Error()
				effective.RetentionDays = previous.RetentionDays
				effective.HistoryLimitMB = previous.HistoryLimitMB
			}
		} else {
			if err := a.store.Clear(); err != nil {
				errs["keepHistory"] = err.Error()
				effective.KeepHistory = true
			}
			// Clear can update the index before a file removal fails.
			historyChanged = true
		}
	}
	a.mu.Lock()
	a.settings = effective
	a.mu.Unlock()
	if historyChanged {
		a.emitHistoryChanged()
	}
	return errs
}
