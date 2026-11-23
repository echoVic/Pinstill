package app

import (
	"math"
	"slices"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/egoist/mygo"
)

type overlaySurface struct {
	window  *mygo.Window
	display mygo.Display
	bounds  mygo.Rectangle
	windows []OverlayWindow
	cursor  mygo.Point
	session uint64
	ready   bool
}

func (a *App) openOverlaySurfaces(ordered []mygo.Display, rawWindows []capture.WindowInfo, cursor mygo.Point, session uint64) {
	for len(a.overlayPool) < len(ordered) {
		a.overlayPool = append(a.overlayPool, a.newOverlayWindow())
	}
	surfaces := make([]overlaySurface, 0, len(ordered))
	for index, display := range ordered {
		window := a.overlayPool[index]
		area := captureOverlayBounds(display)
		window.SetOpacity(0)
		window.SetContentBounds(area)
		window.ShowInactive()
		actual := window.ContentBounds()
		if actual.Width <= 0 || actual.Height <= 0 {
			actual = area
		}
		surfaces = append(surfaces, overlaySurface{
			window: window, display: display, bounds: actual, cursor: cursor, session: session,
			windows: projectOverlayWindows(rawWindows, asCaptureRect(actual)),
		})
	}
	active := surfaces[0]
	a.mu.Lock()
	a.clearOverlayRevealLocked()
	a.overlay = active.window
	a.overlayDisplay = active.display
	a.overlayRect = active.bounds
	a.overlaySurfaces = surfaces
	a.overlayInteraction = session
	a.overlaySource = nil
	a.overlayCommitted = false
	a.overlaySession = session
	a.overlayRevealTimer = time.AfterFunc(120*time.Millisecond, func() {
		a.revealOverlay(session)
	})
	a.mu.Unlock()
	active.window.Focus()
	for _, surface := range surfaces {
		_ = EventOverlayShown.Emit(surface.window, a.overlayInfoFor(surface.window))
	}
}

func activeOverlayDisplays(displays []mygo.Display, cursor mygo.Point) []mygo.Display {
	activeIndex := -1
	best := math.Inf(1)
	for index, display := range displays {
		bounds := display.Bounds
		if bounds.Width <= 0 || bounds.Height <= 0 {
			continue
		}
		distanceX := max(float64(bounds.X-cursor.X), float64(cursor.X-(bounds.X+bounds.Width-1)), 0)
		distanceY := max(float64(bounds.Y-cursor.Y), float64(cursor.Y-(bounds.Y+bounds.Height-1)), 0)
		if distance := distanceX*distanceX + distanceY*distanceY; distance < best {
			activeIndex, best = index, distance
		}
	}
	if activeIndex < 0 {
		return nil
	}
	ordered := []mygo.Display{displays[activeIndex]}
	if !supportsAcrossDisplayOverlay {
		return ordered
	}
	for _, display := range displays {
		if display.Bounds.Width <= 0 || display.Bounds.Height <= 0 {
			continue
		}
		if slices.ContainsFunc(ordered, func(existing mygo.Display) bool { return existing.Bounds == display.Bounds }) {
			continue
		}
		ordered = append(ordered, display)
	}
	return ordered
}

func asCaptureRect(bounds mygo.Rectangle) capture.Rect {
	return capture.Rect{X: float64(bounds.X), Y: float64(bounds.Y), Width: float64(bounds.Width), Height: float64(bounds.Height)}
}

func relativeOverlayRect(rect capture.Rect, bounds mygo.Rectangle) Rect {
	return Rect{X: rect.X - float64(bounds.X), Y: rect.Y - float64(bounds.Y), Width: rect.Width, Height: rect.Height}
}

func projectOverlayRect(rect Rect, from, to mygo.Rectangle) Rect {
	rect.X += float64(from.X - to.X)
	rect.Y += float64(from.Y - to.Y)
	return rect
}

func displayUnion(displays []capture.DisplaySurface) mygo.Rectangle {
	if len(displays) == 0 {
		return mygo.Rectangle{}
	}
	first := displays[0].Bounds
	left, top := int(first.X), int(first.Y)
	right, bottom := left+int(first.Width), top+int(first.Height)
	for _, display := range displays[1:] {
		bounds := display.Bounds
		left, top = min(left, int(bounds.X)), min(top, int(bounds.Y))
		right, bottom = max(right, int(bounds.X+bounds.Width)), max(bottom, int(bounds.Y+bounds.Height))
	}
	return mygo.Rectangle{X: left, Y: top, Width: right - left, Height: bottom - top}
}

func (a *App) overlaySurfaceFor(caller *mygo.Window) (overlaySurface, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if caller == nil {
		caller = a.overlay
	}
	for _, surface := range a.overlaySurfaces {
		if surface.window == caller {
			return surface, true
		}
	}
	return overlaySurface{}, false
}

func (a *App) overlayInfoOnSurfaceLocked(surface overlaySurface) OverlayInfo {
	area := surface.bounds
	var info OverlayInfo
	info.Session = surface.session
	info.DisplayX, info.DisplayY = area.X, area.Y
	info.Width, info.Height = area.Width, area.Height
	info.Scale = surface.display.ScaleFactor
	info.CursorX, info.CursorY = float64(surface.cursor.X-area.X), float64(surface.cursor.Y-area.Y)
	info.HasCursor = info.CursorX >= 0 && info.CursorY >= 0 && info.CursorX < float64(area.Width) && info.CursorY < float64(area.Height)
	info.Windows = slices.Clone(surface.windows)
	if len(a.overlaySurfaces) > 1 {
		displays := a.captureDisplaysLocked()
		desktop := relativeOverlayRect(asCaptureRect(displayUnion(displays)), area)
		info.Desktop = &desktop
		for _, display := range displays {
			info.Displays = append(info.Displays, OverlayDisplay{Rect: relativeOverlayRect(display.Bounds, area), Scale: display.Scale})
		}
	}
	return info
}

func (a *App) captureDisplaysLocked() []capture.DisplaySurface {
	var displays []capture.DisplaySurface
	for _, surface := range a.overlaySurfaces {
		displays = append(displays, capture.DisplaySurface{Bounds: asCaptureRect(surface.bounds), Scale: surface.display.ScaleFactor})
	}
	return displays
}

func (a *App) overlayPanesLocked() []*mygo.Window {
	var windows []*mygo.Window
	for _, surface := range a.overlaySurfaces {
		windows = append(windows, surface.window)
	}
	if len(windows) == 0 && a.overlay != nil {
		windows = append(windows, a.overlay)
	}
	return windows
}

func (a *App) previewOverlaySelection(caller *mygo.Window, session uint64, rect *Rect) {
	a.runOverlayUI(func() {
		a.mu.Lock()
		if !a.capturing || a.quitting || a.overlayCommitted || session != a.overlayInteraction || session == 0 || (a.overlaySource != nil && caller != a.overlaySource) {
			a.mu.Unlock()
			return
		}
		var origin mygo.Rectangle
		for _, surface := range a.overlaySurfaces {
			if surface.window == caller {
				origin = surface.bounds
				break
			}
		}
		if origin.Width <= 0 || origin.Height <= 0 {
			a.mu.Unlock()
			return
		}
		a.overlaySource = caller
		if rect == nil {
			a.overlaySource = nil
		}
		surfaces := slices.Clone(a.overlaySurfaces)
		a.mu.Unlock()
		if !caller.IsFocused() {
			caller.Focus()
		}
		for _, surface := range surfaces {
			if surface.window == caller {
				continue
			}
			message := OverlayPreviewPayload{Session: session}
			if rect != nil {
				local := projectOverlayRect(*rect, origin, surface.bounds)
				message.Rect = &local
			}
			_ = EventOverlayPreview.Emit(surface.window, message)
		}
	})
}

func (a *App) commitOverlaySurface(caller *mygo.Window) bool {
	committed := false
	a.runOverlayUI(func() {
		a.mu.Lock()
		if !a.capturing || a.quitting || a.scroll != nil || (a.overlayCommitted && caller != a.overlay) || (a.overlaySource != nil && caller != a.overlaySource) {
			a.mu.Unlock()
			return
		}
		var active overlaySurface
		for _, surface := range a.overlaySurfaces {
			if surface.window == caller {
				active = surface
				break
			}
		}
		if active.window == nil {
			a.mu.Unlock()
			return
		}
		a.overlay = active.window
		a.overlayRect = active.bounds
		a.overlayDisplay = active.display
		a.overlayCommitted = true
		a.clearOverlayRevealLocked()
		windows := a.overlayPanesLocked()
		a.mu.Unlock()
		caller.SetOpacity(1)
		for _, window := range windows {
			if window != caller && window.IsVisible() {
				window.SetOpacity(0)
				window.Hide()
			}
		}
		if !caller.IsFocused() {
			caller.Focus()
		}
		committed = true
	})
	return committed
}
