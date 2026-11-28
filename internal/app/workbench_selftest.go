package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
)

func (a *App) selftestWorkbench() {
	selftestRequire(runtime.GOOS == "darwin" && a.main != nil && a.translate != nil, "workbench test requires a macOS bundle")
	executable, err := os.Executable()
	selftestCheckError(err)
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	selftestRequire(filepath.Ext(bundle) == ".app", "workbench test requires an app bundle")
	waitFor := func(description string, condition func() bool) {
		deadline := time.Now().Add(5 * time.Second)
		for !condition() && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		if !condition() {
			selftestFail("%s (active=%v hidden=%v main_visible=%v main_focused=%v main_minimized=%v)",
				description, mygo.App.IsActive(), mygo.App.IsHidden(), a.main.IsVisible(), a.main.IsFocused(), a.main.IsMinimized())
		}
	}
	mainRestored := func() bool { return a.main.IsVisible() && !a.main.IsMinimized() }
	waitFor("main window did not load", mainRestored)
	a.main.Close()
	selftestRequire(!a.main.IsVisible() && !a.main.IsDestroyed(), "closing should hide the workbench without quitting")
	reopened := make(chan struct{}, 1)
	offReopen := mygo.App.OnActivate(func(bool) {
		select {
		case reopened <- struct{}{}:
		default:
		}
	})
	defer offReopen()
	selftestCheckError(exec.Command("/usr/bin/open", "-a", bundle).Run())
	select {
	case <-reopened:
	case <-time.After(5 * time.Second):
		selftestFail("Launch Services did not deliver a reopen event")
	}
	waitFor("reopening the app did not bring back the closed workbench", mainRestored)
	fmt.Println("SELFTEST workbench: reopen=true")

	a.main.Close()
	mygo.RunOnMain(a.reopenWorkbench)
	waitFor("the activation handler did not bring back the closed workbench", mainRestored)
	fmt.Println("SELFTEST workbench: activation_handler=true")

	minimized := make(chan struct{}, 1)
	offMinimize := a.main.OnMinimize(func() { minimized <- struct{}{} })
	a.main.Minimize()
	select {
	case <-minimized:
	case <-time.After(5 * time.Second):
		selftestFail("main window did not finish minimizing")
	}
	offMinimize()
	a.showMain()
	waitFor("explicit workbench request did not restore the minimized window", mainRestored)
	fmt.Println("SELFTEST workbench: restore=true")

	a.main.Close()
	mygo.App.Hide()
	waitFor("app did not leave the foreground before translation", func() bool { return !mygo.App.IsActive() })
	a.ShowTranslate()
	waitFor("translation window did not open", a.translate.IsVisible)
	mygo.RunOnMain(a.reopenWorkbench)
	selftestRequire(!a.main.IsVisible(), "showing translation also opened the workbench")
	fmt.Println("SELFTEST workbench: translate_kept_main_hidden=true")

	a.translate.Close()
	mygo.App.Hide()
	waitFor("app did not leave the foreground before pin management", func() bool { return !mygo.App.IsActive() })
	a.showPinManager()
	waitFor("pin manager did not open", func() bool {
		a.mu.Lock()
		window := a.pinManager
		a.mu.Unlock()
		return window != nil && window.IsVisible()
	})
	mygo.RunOnMain(a.reopenWorkbench)
	selftestRequire(!a.main.IsVisible(), "showing pin management also opened the workbench")
	fmt.Println("SELFTEST workbench: pin_manager_kept_main_hidden=true")
}
