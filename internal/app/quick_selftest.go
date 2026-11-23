package app

import (
	"bytes"
	"fmt"
	"time"

	"github.com/egoist/mygo"
)

func (a *App) selftestQuick() {
	png := syntheticScreenshot()
	width, height := imageDimensions(png)
	requested := make(chan string, 1)
	a.captureAction = func(action string, input []byte, payload capturePayload) (CaptureActionResult, error) {
		if !bytes.Equal(input, png) || payload.Width != width || payload.Height != height {
			return CaptureActionResult{}, fmt.Errorf("快捷操作收到其他截图")
		}
		requested <- action
		return CaptureActionResult{Completed: true}, nil
	}
	a.mu.Lock()
	generation := a.captureGeneration
	a.mu.Unlock()
	a.postQuickCapture(generation, "copy", png, capturePayload{Width: width, Height: height})
	a.mu.Lock()
	item := a.quick
	a.mu.Unlock()
	selftestRequire(item != nil, "quick capture did not create a session")
	deadline := time.Now().Add(8 * time.Second)
	for !item.window.IsVisible() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	selftestRequire(item.window.IsVisible() && !item.window.IsFocused(), "quick capture must open without asking for focus")
	result, err := mygo.EvalAs[bool](item.window.Page(), `(() => {
		const image = document.getElementById('quickThumbnail');
		return image.complete && image.naturalWidth > 0 &&
		  ['copy', 'save', 'pin', 'hang'].every(action => {
		    const button = document.querySelector('[data-quick-action="' + action + '"]');
		    const bounds = button?.getBoundingClientRect();
		    return bounds && bounds.width > 0 && bounds.right <= innerWidth && bounds.bottom <= innerHeight;
		  });
	})()`)
	selftestCheckError(err)
	selftestRequire(result, "quick capture thumbnail or actions are not visible")
	shot, err := item.window.CapturePage()
	selftestCheckError(err)
	selftestArtifact("pinstill-quick.png", shot)
	_, err = item.window.Page().Eval(`setTimeout(() => document.querySelector('[data-quick-action="hang"]').click(), 100); true`)
	selftestCheckError(err)
	select {
	case action := <-requested:
		selftestRequire(action == "hang", "quick capture sent action %s", action)
	case <-time.After(4 * time.Second):
		selftestFail("quick capture did not send the action")
	}
	deadline = time.Now().Add(4 * time.Second)
	for !item.window.IsDestroyed() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	selftestRequire(item.window.IsDestroyed(), "quick capture stayed on screen after the action")
	selftestExit(0)
}
