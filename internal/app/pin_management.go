package app

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/egoist/mygo"
)

type PinPresentation struct {
	ID           string `json:"id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Group        string `json:"group"`
	Hidden       bool   `json:"hidden"`
	ClickThrough bool   `json:"clickThrough"`
	Rotation     int    `json:"rotation"`
	FlipX        bool   `json:"flipX"`
	FlipY        bool   `json:"flipY"`
}

type ManagedPin struct {
	PinPresentation
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
}

var (
	EventPinPresentation = mygo.NewEvent[PinPresentation]("pin:presentation")
	EventManagedPins     = mygo.NewEvent[bool]("pin:managed")
)

func pinPresentation(state pin.State) PinPresentation {
	return PinPresentation{
		ID: state.ID, Width: state.Width, Height: state.Height, Group: state.Group, Hidden: state.Hidden,
		ClickThrough: state.ClickThrough, Rotation: state.Rotation, FlipX: state.FlipX, FlipY: state.FlipY,
	}
}

func (s *CaptureService) ListManagedPins() ([]ManagedPin, error) {
	a := s.app
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pinStore == nil {
		return nil, errors.New("贴图存储不可用")
	}
	pins := make([]ManagedPin, 0, len(a.pinStates))
	for _, state := range a.pinStates {
		pins = append(pins, ManagedPin{
			PinPresentation: pinPresentation(state), URL: a.pinURL(state.File), CreatedAt: state.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	sort.Slice(pins, func(left, right int) bool {
		if pins[left].CreatedAt == pins[right].CreatedAt {
			return pins[left].ID > pins[right].ID
		}
		return pins[left].CreatedAt > pins[right].CreatedAt
	})
	return pins, nil
}

func (s *CaptureService) ManagePin(id, action, value string) (PinPresentation, error) {
	return s.app.managePin(id, action, value)
}

func (s *CaptureService) ManagePinGroup(group, action, value string) error {
	return s.app.managePinGroup(group, action, value)
}

func (s *CaptureService) ShowPinManager() { s.app.showPinManager() }

func (a *App) managePin(id, action, value string) (PinPresentation, error) {
	var displays []mygo.Display
	if action == "rotate-right" || action == "rotate-left" {
		a.mu.Lock()
		window := a.pins[id]
		a.mu.Unlock()
		if window != nil {
			displays = mygo.Screen.Displays()
		}
	}
	change, err := managedPinChange(action, value, displays)
	if err != nil {
		return PinPresentation{}, err
	}
	a.mu.Lock()
	if _, exists := a.pinStates[id]; !exists {
		a.mu.Unlock()
		return PinPresentation{}, errors.New("这张贴图已关闭")
	}
	updated, err := a.changeManagedPinsLocked([]string{id}, change)
	a.mu.Unlock()
	if len(updated) == 0 {
		return PinPresentation{}, err
	}
	a.applyManagedPins(updated, action)
	state, _ := a.pinState(id)
	return pinPresentation(state), err
}

func (a *App) managePinGroup(group, action, value string) error {
	group = pin.NormalizeGroup(group)
	switch action {
	case "show", "hide", "unlock", "lock":
	case "rename":
		action = "group"
	default:
		return errors.New("未知的贴图分组操作")
	}
	change, err := managedPinChange(action, value, nil)
	if err != nil {
		return err
	}
	a.mu.Lock()
	var ids []string
	for id, state := range a.pinStates {
		if state.Group == group {
			ids = append(ids, id)
		}
	}
	updated, err := a.changeManagedPinsLocked(ids, change)
	a.mu.Unlock()
	a.applyManagedPins(updated, action)
	return err
}

func managedPinChange(action, value string, displays []mygo.Display) (func(*pin.State), error) {
	switch action {
	case "group":
		label := pin.NormalizeGroup(value)
		return func(state *pin.State) { state.Group = label }, nil
	case "show", "hide":
		return func(state *pin.State) { state.Hidden = action == "hide" }, nil
	case "lock", "unlock":
		return func(state *pin.State) { state.ClickThrough = action == "lock" }, nil
	case "rotate-right", "rotate-left":
		return func(state *pin.State) {
			turn := 1
			if action == "rotate-left" {
				turn = 3
			}
			state.Rotation = (state.Rotation + turn) % 4
			state.X += (state.Width - state.Height) / 2
			state.Y += (state.Height - state.Width) / 2
			state.Width, state.Height = state.Height, state.Width
			*state = clampPinToDisplays(*state, displays)
		}, nil
	case "flip-horizontal", "flip-vertical":
		return func(state *pin.State) {
			if (action == "flip-horizontal") == (state.Rotation%2 == 0) {
				state.FlipX = !state.FlipX
			} else {
				state.FlipY = !state.FlipY
			}
		}, nil
	default:
		return nil, errors.New("未知的贴图操作")
	}
}

func (a *App) changeManagedPinsLocked(ids []string, change func(*pin.State)) ([]pin.State, error) {
	if a.pinStore == nil {
		return nil, errors.New("贴图存储不可用")
	}
	states, err := a.pinStore.UpdateExisting(ids, func(state *pin.State) {
		live := a.pinStates[state.ID]
		state.X, state.Y, state.Width, state.Height = live.X, live.Y, live.Width, live.Height
		change(state)
	})
	for _, state := range states {
		a.pinStates[state.ID] = state
	}
	if err != nil {
		return states, fmt.Errorf("保存贴图状态失败：%w", err)
	}
	return states, nil
}

func (a *App) applyManagedPins(states []pin.State, action string) {
	for _, changed := range states {
		a.applyPinChange(changed.ID, func(state pin.State) {
			a.mu.Lock()
			window, manager := a.pins[state.ID], a.pinManager
			a.mu.Unlock()
			if window == nil || window.IsDestroyed() {
				return
			}
			if action == "rotate-right" || action == "rotate-left" {
				window.SetContentBounds(mygo.Rectangle{X: state.X, Y: state.Y, Width: state.Width, Height: state.Height})
			}
			window.SetIgnoreMouseEvents(state.ClickThrough)
			if state.Hidden && window.IsVisible() {
				window.Hide()
			} else if !state.Hidden && !window.IsVisible() {
				window.ShowInactive()
				if manager != nil && manager.IsFocused() {
					manager.ShowInactive()
				}
			}
			_ = EventPinPresentation.Emit(window, pinPresentation(state))
		})
	}
	if len(states) > 0 {
		a.emitManagedPins()
	}
}

func (a *App) emitManagedPins() {
	a.mu.Lock()
	window := a.pinManager
	a.mu.Unlock()
	emitToWindow(a, window, EventManagedPins, true)
}

func (a *App) showPinManager() {
	mygo.RunOnMain(func() {
		a.mu.Lock()
		window, quitting := a.pinManager, a.quitting
		a.mu.Unlock()
		if quitting {
			return
		}
		if window != nil && !window.IsDestroyed() {
			mygo.App.Focus()
			window.Show()
			window.Focus()
			a.emitManagedPins()
			return
		}
		window = mygo.NewWindow(mygo.WindowOptions{
			Page: mygo.PageOptions{Session: a.pageSession}, Title: "Pinstill · 贴图管理", URL: "/pins.html",
			Width: 760, Height: 520, MinWidth: 548, MinHeight: 360, Hidden: true, AlwaysOnTop: true,
			BackgroundColor: "#171613",
		})
		window.OnReadyToShow(func() {
			mygo.App.Focus()
			window.Show()
			window.Focus()
		})
		window.OnClosed(func() {
			a.mu.Lock()
			if a.pinManager == window {
				a.pinManager = nil
			}
			a.mu.Unlock()
		})
		a.mu.Lock()
		a.pinManager = window
		a.mu.Unlock()
	})
}
