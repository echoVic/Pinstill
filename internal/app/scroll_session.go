package app

import (
	"errors"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/egoist/mygo"
)

// Native window calls wait on the UI thread. Serialize the identity check and
// presentation there, without holding a mutex while waiting for that thread.
func (a *App) runScrollUI(fn func()) {
	if a.scrollUI != nil {
		a.scrollUI(fn)
		return
	}
	a.mu.Lock()
	native := a.control != nil || a.scrollFrame != nil || a.result != nil
	a.mu.Unlock()
	if native {
		mygo.RunOnMain(fn)
	} else {
		fn()
	}
}

func (a *App) scrollResultIsCurrent(session *scrollSession) bool {
	a.mu.Lock()
	active := a.scroll == session && !a.quitting
	a.mu.Unlock()
	if !active {
		return false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return !session.cancelled
}

func (a *App) currentScroll(id string) (*scrollSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id == "" || a.scroll == nil || a.scroll.id != id {
		return nil, errors.New("滚动截图会话已结束")
	}
	return a.scroll, nil
}

func (a *App) setScrollPausedFor(id string, paused bool) error {
	a.scrollMu.Lock()
	defer a.scrollMu.Unlock()
	if _, err := a.currentScroll(id); err != nil {
		return err
	}
	return a.setScrollPaused(paused)
}

func (a *App) setScrollAutoFor(id string, enabled bool) error {
	a.scrollMu.Lock()
	defer a.scrollMu.Unlock()
	if _, err := a.currentScroll(id); err != nil {
		return err
	}
	return a.setScrollAuto(enabled)
}

func (a *App) finishScrollFor(id, action string) error {
	if id == "" {
		return errors.New("滚动截图会话已结束")
	}
	return a.finishScrollIdentity(id, action)
}

func (a *App) withScrollChromeHidden(session *scrollSession, hide, show func(), shot func() error, delay time.Duration) error {
	active := false
	a.runScrollUI(func() {
		active = a.scrollResultIsCurrent(session) && !session.isStopped()
		if active {
			hide()
		}
	})
	if !active {
		return errors.New("滚动截图会话已结束")
	}
	defer func() {
		a.runScrollUI(func() {
			if a.scrollResultIsCurrent(session) && !session.isStopped() {
				show()
			}
		})
	}()
	if delay > 0 {
		time.Sleep(delay)
	}
	return shot()
}

func (a *App) captureScrollFrame(session *scrollSession) ([]byte, error) {
	a.mu.Lock()
	control, frame := a.control, a.scrollFrame
	a.mu.Unlock()
	controlVisible := control != nil && !control.IsDestroyed() && control.IsVisible()
	frameVisible := frame != nil && !frame.IsDestroyed() && frame.IsVisible()
	var data []byte
	err := a.withScrollChromeHidden(session, func() {
		if controlVisible {
			control.Hide()
		}
		if frameVisible {
			frame.Hide()
		}
	}, func() {
		if controlVisible && !control.IsDestroyed() {
			control.Show()
		}
		if frameVisible && !frame.IsDestroyed() {
			frame.Show()
		}
	}, func() (err error) {
		data, err = a.captureRegion(session.region, capture.CaptureOptions{IncludeCursor: false})
		return err
	}, 20*time.Millisecond)
	return data, err
}
