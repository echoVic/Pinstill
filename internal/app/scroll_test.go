package app

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/stitch"
	"github.com/egoist/mygo"
)

func TestManualScrollWaitsOnStaticContentWithoutUsingFrameBudget(test *testing.T) {
	test.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	test.Setenv("PINSTILL_SELFTEST_SCROLL_SIM", "")
	synctest.Test(test, func(test *testing.T) {
		session := &scrollSession{
			region: capture.Rect{Width: 120, Height: 120},
			acc:    stitch.New(stitch.Options{}),
			stop:   make(chan struct{}),
			done:   make(chan struct{}),
		}
		app := &App{scroll: session, capturing: true, settings: Settings{KeepHistory: true}}
		go app.scrollLoop(session)
		defer func() {
			close(session.stop)
			<-session.done
		}()

		time.Sleep(scrollInitialDelay + 4*scrollFrameInterval)
		synctest.Wait()
		frames, _, status, auto := session.snapshot()
		if session.isFinalized() || auto || status != "running" || frames != 1 {
			test.Fatalf("waiting on static content: finalized=%v, auto=%v, status=%q, frames=%d", session.isFinalized(), auto, status, frames)
		}
	})
}

func TestManualScrollStaysOpenAfterMovementStops(test *testing.T) {
	test.Setenv("PINSTILL_SELFTEST_SCROLL_SIM", "1")
	simulatedScrollCounter.Store(0)
	synctest.Test(test, func(test *testing.T) {
		session := &scrollSession{
			region: capture.Rect{Width: 520, Height: 420},
			acc:    stitch.New(stitch.Options{}),
			stop:   make(chan struct{}),
			done:   make(chan struct{}),
		}
		app := &App{scroll: session, capturing: true, settings: Settings{KeepHistory: true}}
		go app.scrollLoop(session)
		defer func() {
			close(session.stop)
			<-session.done
		}()

		time.Sleep(6 * time.Second)
		synctest.Wait()
		frames, height, status, auto := session.snapshot()
		if session.isFinalized() || auto || status != "stalled" || frames != 7 || height != 2600 {
			test.Fatalf("manual capture after the pause: finalized=%v auto=%v status=%q frames=%d height=%d", session.isFinalized(), auto, status, frames, height)
		}
	})
}

func TestAutomaticScrollWithoutProgressLeavesCaptureOpen(test *testing.T) {
	test.Setenv("PINSTILL_SELFTEST_SCROLL_SIM", "1")
	simulatedScrollCounter.Store(100)
	synctest.Test(test, func(test *testing.T) {
		session := &scrollSession{
			region: capture.Rect{Width: 520, Height: 420},
			auto:   true,
			acc:    stitch.New(stitch.Options{}),
			stop:   make(chan struct{}),
			done:   make(chan struct{}),
		}
		app := &App{scroll: session, capturing: true, settings: Settings{KeepHistory: true}}
		go app.scrollLoop(session)
		defer func() {
			close(session.stop)
			<-session.done
		}()

		time.Sleep(6 * time.Second)
		synctest.Wait()
		frames, _, status, auto := session.snapshot()
		if session.isFinalized() || auto || status != "stalled" || frames != 1 {
			test.Fatalf("automatic capture without movement: finalized=%v auto=%v status=%q frames=%d", session.isFinalized(), auto, status, frames)
		}
	})
}

func TestFinishScrollReleasesStoppedSession(t *testing.T) {
	for _, action := range []string{"finish", "cancel"} {
		t.Run(action, func(t *testing.T) {
			// Finishing before the first frame arrives must still leave capture
			// mode, so the next screenshot can start. No native windows needed.
			session := &scrollSession{
				acc:  stitch.New(stitch.Options{}),
				stop: make(chan struct{}), done: make(chan struct{}),
			}
			close(session.done)
			a := &App{scroll: session, capturing: true, restoreMainAfterCapture: true}
			if err := a.finishScroll(action); err != nil {
				t.Fatal(err)
			}
			if a.scroll != nil || a.capturing || a.restoreMainAfterCapture {
				t.Fatal("finished scrolling capture still blocks the next screenshot")
			}
			if got, want := session.isFinalized(), action == "finish"; got != want {
				t.Fatalf("finalized = %v, want %v", got, want)
			}
		})
	}
}

func TestOldScrollFinalizerDoesNotClearNewSession(t *testing.T) {
	old := &scrollSession{acc: stitch.New(stitch.Options{})}
	current := &scrollSession{}
	a := &App{scroll: current, capturing: true}
	a.finalizeScroll(old, "stalled", 0)
	if a.scroll != current || !a.capturing {
		t.Fatal("an old scroll completion cleared the newer capture")
	}
}

func TestCancelledScrollDoesNotAutoFinalize(t *testing.T) {
	session := &scrollSession{stopped: true, acc: stitch.New(stitch.Options{})}
	a := &App{scroll: session, capturing: true}
	a.finalizeScroll(session, "stalled", 0)
	if session.isFinalized() {
		t.Fatal("cancelled scrolling capture was finalized by the capture loop")
	}
}

func TestAutoEndCannotFinishAfterReturningToManual(t *testing.T) {
	session := &scrollSession{auto: false, mode: 2, acc: stitch.New(stitch.Options{})}
	app := &App{scroll: session, capturing: true}
	app.finalizeScroll(session, "stalled", 1)
	if session.isFinalized() || app.scroll != session {
		t.Fatal("an automatic end replaced the manual capture")
	}
}

type fakeScrollChrome struct {
	visible      bool
	destroyed    bool
	bounds       mygo.Rectangle
	hides        int
	showInactive int
}

func (f *fakeScrollChrome) IsDestroyed() bool      { return f.destroyed }
func (f *fakeScrollChrome) IsVisible() bool        { return f.visible }
func (f *fakeScrollChrome) Bounds() mygo.Rectangle { return f.bounds }
func (f *fakeScrollChrome) Hide()                  { f.hides++; f.visible = false }
func (f *fakeScrollChrome) ShowInactive()          { f.showInactive++; f.visible = true }

func TestScrollFrameBounds(t *testing.T) {
	got := scrollFrameBounds(capture.Rect{X: 60, Y: 80, Width: 520, Height: 420})
	want := mygo.Rectangle{X: 58, Y: 78, Width: 524, Height: 424}
	if got != want {
		t.Fatalf("scrollFrameBounds integer = %+v, want %+v", got, want)
	}

	gotFrac := scrollFrameBounds(capture.Rect{X: 10.6, Y: 20.4, Width: 300.7, Height: 200.2})
	wantFrac := mygo.Rectangle{X: 9, Y: 18, Width: 305, Height: 204}
	if gotFrac != wantFrac {
		t.Fatalf("scrollFrameBounds fractional = %+v, want %+v", gotFrac, wantFrac)
	}
}

func TestCaptureScrollFrameKeepsNonOverlappingChromeVisible(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	session := &scrollSession{
		id:     "scroll-1",
		region: capture.Rect{X: 100, Y: 100, Width: 400, Height: 300},
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	chrome := &fakeScrollChrome{
		visible: true,
		bounds:  mygo.Rectangle{X: 110, Y: 412, Width: 380, Height: 64},
	}
	a := &App{scroll: session, capturing: true}
	data, err := a.captureScrollFrameWith(session, chrome, 0)
	if err != nil || len(data) == 0 {
		t.Fatalf("captureScrollFrameWith() = %d bytes, %v", len(data), err)
	}
	if chrome.hides != 0 || chrome.showInactive != 0 || !chrome.visible {
		t.Fatalf("non-overlapping control flickered: hides=%d showInactive=%d visible=%v", chrome.hides, chrome.showInactive, chrome.visible)
	}
}

func TestCaptureScrollFrameHidesOnlyOverlappingControlAndRestoresInactive(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	region := capture.Rect{X: 0, Y: 0, Width: 1200, Height: 800}

	session := &scrollSession{id: "scroll-1", region: region, stop: make(chan struct{}), done: make(chan struct{})}
	chrome := &fakeScrollChrome{
		visible: true,
		bounds:  mygo.Rectangle{X: 16, Y: 16, Width: 380, Height: 64},
	}
	a := &App{scroll: session, capturing: true}
	data, err := a.captureScrollFrameWith(session, chrome, 0)
	if err != nil || len(data) == 0 {
		t.Fatalf("captureScrollFrameWith() overlapping = %d bytes, %v", len(data), err)
	}
	if chrome.hides != 1 || chrome.showInactive != 1 || !chrome.visible {
		t.Fatalf("overlapping control lifecycle: hides=%d showInactive=%d visible=%v", chrome.hides, chrome.showInactive, chrome.visible)
	}

	// If the session is replaced mid-capture, the overlapping control must not
	// be shown back on behalf of the stale session.
	stale := &scrollSession{id: "scroll-2", region: region, stop: make(chan struct{}), done: make(chan struct{})}
	chrome2 := &fakeScrollChrome{
		visible: true,
		bounds:  mygo.Rectangle{X: 16, Y: 16, Width: 380, Height: 64},
	}
	calls := 0
	a2 := &App{scroll: stale, capturing: true}
	a2.scrollUI = func(fn func()) {
		calls++
		if calls == 3 {
			a2.scroll = &scrollSession{id: "scroll-3"}
		}
		fn()
	}
	if _, err := a2.captureScrollFrameWith(stale, chrome2, 0); err != nil {
		t.Fatalf("captureScrollFrameWith() stale = %v", err)
	}
	if chrome2.hides != 1 || chrome2.showInactive != 0 {
		t.Fatalf("stale session restored control: hides=%d showInactive=%d", chrome2.hides, chrome2.showInactive)
	}
}

func TestCaptureScrollFrameRejectsStoppedSession(t *testing.T) {
	stopped := &scrollSession{
		id:      "scroll-stopped",
		region:  capture.Rect{X: 100, Y: 100, Width: 400, Height: 300},
		stopped: true,
	}
	chrome := &fakeScrollChrome{
		visible: true,
		bounds:  mygo.Rectangle{X: 120, Y: 120, Width: 380, Height: 64},
	}
	a := &App{scroll: stopped, capturing: true}
	if _, err := a.captureScrollFrameWith(stopped, chrome, 0); err == nil {
		t.Fatal("expected error for stopped session")
	}
	if chrome.hides != 0 || chrome.showInactive != 0 {
		t.Fatalf("stopped session mutated chrome: hides=%d showInactive=%d", chrome.hides, chrome.showInactive)
	}
}
