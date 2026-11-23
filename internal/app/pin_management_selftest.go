package app

import (
	"bytes"
	"fmt"
	"strconv"
	"time"

	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/egoist/mygo"
)

func (a *App) selftestPinManager() {
	png := syntheticScreenshot()
	first, err := a.pinPNG(png, 0, 0)
	selftestCheckError(err)
	second, err := a.pinPNG(png, 0, 0)
	selftestCheckError(err)
	a.mu.Lock()
	firstWindow, secondWindow := a.pins[first], a.pins[second]
	a.mu.Unlock()
	selftestWaitPinManager("both pins to open", func() bool {
		return firstWindow.IsVisible() && secondWindow.IsVisible()
	})

	openManager := func() *mygo.Window {
		a.showPinManager()
		a.mu.Lock()
		window := a.pinManager
		a.mu.Unlock()
		selftestRequire(window != nil, "pin manager window was not created")
		selftestWaitPinManager("manager to render both pins and thumbnails", func() bool {
			return window.IsVisible() && selftestPinManagerEval(window, `(() => {
				const pictures = [...document.querySelectorAll('.pin-row img')];
				return pictures.length === 2 && pictures.every(picture => picture.complete && picture.naturalWidth > 0);
			})()`)
		})
		return window
	}
	manager := openManager()
	assignGroup := func(key, label string) {
		selftestRequire(selftestPinManagerEval(manager, fmt.Sprintf(`(() => {
			const editor = [...document.querySelectorAll('input[data-control]')].find(input => input.dataset.control === %s);
			if (!editor || !editor.form || editor.closest('[hidden]')) return false;
			editor.value = %s;
			editor.form.requestSubmit();
			return true;
		})()`, strconv.Quote(key+":input"), strconv.Quote(label))), "missing group editor %s", key)
		selftestWaitPinManager("group change to finish", func() bool {
			return selftestPinManagerEval(manager, `document.getElementById('pinsList').getAttribute('aria-busy') === 'false'`)
		})
	}
	const draftGroup, group = "设计草图", "设计评审"
	assignGroup(first+":group", draftGroup)
	assignGroup(second+":group", draftGroup)
	selftestClickPinManager(manager, "group:"+draftGroup+":rename")
	assignGroup("group:"+draftGroup+":rename", group)

	selftestClickPinManager(manager, "group:"+group+":hide")
	selftestWaitPinManager("native pins to hide", func() bool {
		return !firstWindow.IsVisible() && !secondWindow.IsVisible()
	})
	selftestClickPinManager(manager, "group:"+group+":show")
	selftestWaitPinManager("native pins to show", func() bool {
		return firstWindow.IsVisible() && secondWindow.IsVisible()
	})

	clickPinMenu := func(action string) {
		selftestRequire(selftestPinManagerEval(firstWindow, fmt.Sprintf(`(() => {
			document.getElementById('pinWindow').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 10, clientY: 10 }));
			const menu = document.getElementById('pinMenu');
			const button = menu.querySelector('[data-menu-action="%s"]');
			if (menu.hidden || !button) return false;
			button.click();
			return true;
		})()`, action)), "pin context menu action missing: %s", action)
	}
	clickPinMenu("rotate-right")
	selftestWaitPinManager("rotation to reach the native window and image", func() bool {
		bounds := firstWindow.ContentBounds()
		return bounds.Width < bounds.Height && selftestPinManagerEval(firstWindow, `document.getElementById('pinImage').style.transform.includes('rotate(90deg)')`)
	})
	clickPinMenu("flip-horizontal")
	selftestWaitPinManager("flip to reach the pin image", func() bool {
		return selftestPinManagerEval(firstWindow, `document.getElementById('pinImage').style.transform.includes('scale(1, -1)')`)
	})
	selftestClickPinManager(manager, first+":click")
	selftestClickPinManager(manager, second+":click")
	selftestWaitPinManager("both pins to become click-through", func() bool {
		return a.selftestPinClickThrough(first, firstWindow, true) && a.selftestPinClickThrough(second, secondWindow, true)
	})
	manager.Close()
	selftestWaitPinManager("the original manager to close", manager.IsDestroyed)
	manager = openManager()
	shot, err := manager.CapturePage()
	selftestCheckError(err)
	selftestArtifact("pinstill-pin-manager.png", shot)
	selftestClickPinManager(manager, "group:"+group+":unlock")
	selftestWaitPinManager("group to recover native clicks", func() bool {
		return a.selftestPinClickThrough(first, firstWindow, false) && a.selftestPinClickThrough(second, secondWindow, false)
	})

	store, err := pin.Open(a.pinStore.Dir())
	selftestCheckError(err)
	states := store.List()
	selftestRequire(len(states) == 2, "expected two persisted pins, got %d", len(states))
	for _, state := range states {
		selftestRequire(state.Group == group && !state.Hidden && !state.ClickThrough, "incorrect persisted group or visibility: %+v", state)
		if state.ID == first {
			bounds := firstWindow.ContentBounds()
			selftestRequire(state.Rotation == 1 && state.FlipY && state.Width == bounds.Width && state.Height == bounds.Height,
				"persisted rotation or native size is wrong: state=%+v bounds=%+v", state, bounds)
		}
		original, err := store.ReadImage(state)
		selftestCheckError(err)
		selftestRequire(bytes.Equal(original, png), "pin appearance changed the original PNG")
	}
	fmt.Printf("SELFTEST pinmanager: group=%s pins=%d recovery=ok rotation=ok\n", group, len(states))
}

func (a *App) selftestPinClickThrough(id string, window *mygo.Window, expected bool) bool {
	state, exists := a.pinState(id)
	actual, nativeSupported := nativePinClickThrough(window)
	return exists && state.ClickThrough == expected && (!nativeSupported || actual == expected)
}

func selftestWaitPinManager(label string, ready func() bool) {
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	selftestFail("pinmanager timed out waiting for %s", label)
}

func selftestPinManagerEval(window *mygo.Window, script string) bool {
	value, err := mygo.EvalAs[bool](window.Page(), script)
	selftestCheckError(err)
	return value
}

func selftestClickPinManager(window *mygo.Window, key string) {
	selftestWaitPinManager("pin manager control "+key, func() bool {
		return selftestPinManagerEval(window, fmt.Sprintf(`(() => {
			if (document.getElementById('pinsList').getAttribute('aria-busy') !== 'false') return false;
			const button = [...document.querySelectorAll('button[data-control]')].find(control => control.dataset.control === %s);
			if (!button || button.disabled || button.closest('[hidden]')) return false;
			button.click();
			return true;
		})()`, strconv.Quote(key)))
	})
}
