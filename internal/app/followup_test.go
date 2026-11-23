package app

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/stitch"
	"github.com/egoist/mygo"
)

func TestCancelledPreparationSkipsCaptureAndPreservesPreview(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	synctest.Test(t, func(t *testing.T) {
		application := &App{
			capturing:          true,
			settings:           DefaultSettings(),
			sessionDir:         t.TempDir(),
			overlayDisplay:     mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}},
			overlaySessionFile: "previous.png",
		}
		previous := []byte("previous preview")
		if err := os.WriteFile(filepath.Join(application.sessionDir, application.overlaySessionFile), previous, 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		start := time.Now()
		service := &CaptureService{app: application}
		if _, err := service.PrepareSelection(ctx, Rect{Width: 80, Height: 50}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("preparation result: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= 90*time.Millisecond {
			t.Fatalf("canceled preparation waited for the compositor: %v", elapsed)
		}
		files, err := os.ReadDir(application.sessionDir)
		if err != nil || len(files) != 1 || files[0].Name() != "previous.png" || application.overlaySessionFile != "previous.png" {
			t.Fatalf("canceled preparation changed the cache: %v, %v", files, err)
		}
	})
}

func TestFinishInvalidAnnotationRemainsEditable(t *testing.T) {
	a := newTestApp(t)
	a.capturing = true
	if _, err := a.finishAnnotated("copy", "invalid base64!"); err == nil {
		t.Fatal("invalid image accepted")
	}
	if !a.capturing {
		t.Fatal("validation failure discarded editable capture")
	}
}

func TestTruncatedPNGDoesNotReachHistoryOrCaptureAction(t *testing.T) {
	for _, keepHistory := range []bool{true, false} {
		t.Run(map[bool]string{true: "with history", false: "without history"}[keepHistory], func(t *testing.T) {
			application := newTestApp(t)
			application.settings.KeepHistory = keepHistory
			application.capturing = true
			application.captureAction = func(string, []byte, capturePayload) (CaptureActionResult, error) {
				t.Fatal("truncated PNG reached the capture action")
				return CaptureActionResult{}, nil
			}
			png := gnPNGData(t, 80, 40)
			result, err := application.finishAnnotated("copy", base64.StdEncoding.EncodeToString(png[:33]))
			if err == nil || result.Completed || !application.capturing || len(application.store.List()) != 0 {
				t.Fatalf("truncated annotation changed session or history: %+v, %v", result, err)
			}
		})
	}
}

func TestScreenCaptureRejectsTransparentFrameBeforeHistory(t *testing.T) {
	application := newTestApp(t)
	for _, keepHistory := range []bool{true, false} {
		application.settings.KeepHistory = keepHistory
		payload, err := application.storeScreenCapture(gnTransparentPNG(t, 200, 200))
		if !errors.Is(err, errEmptyScreenCapture) || payload.ID != "" || len(application.store.List()) != 0 {
			t.Fatalf("transparent screen capture (history=%v): %+v, %v", keepHistory, payload, err)
		}
	}
	application.settings.KeepHistory = true
	payload, err := application.storeScreenCapture(gnTransparentPNGWithPixel(t, 200, 200, 1, 1))
	if err != nil || payload.HistoryID == "" || payload.Width != 200 || payload.Height != 200 {
		t.Fatalf("visible pixel should survive the blank-frame check: %+v, %v", payload, err)
	}
}

func TestImageHeaderBudgetBeforeDecode(t *testing.T) {
	raw := gnPNGData(t, 1, 1)
	binary.BigEndian.PutUint32(raw[16:20], 100000)
	binary.BigEndian.PutUint32(raw[20:24], 100000)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	if _, _, err := normalizeExport(raw, DefaultSettings()); err == nil || !strings.Contains(err.Error(), "pixel limit") {
		t.Fatalf("export did not preflight header: %v", err)
	}
	a := newTestApp(t)
	if _, err := a.storeCapture(raw); err == nil || !strings.Contains(err.Error(), "pixel limit") {
		t.Fatalf("capture did not preflight: %v", err)
	}
}

func TestFinishActionFailureAndCancellationCanRetryWithoutDuplicateHistory(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancel"}[cancel], func(t *testing.T) {
			a := newTestApp(t)
			a.capturing = true
			raw := base64.StdEncoding.EncodeToString(gnPNGData(t, 24, 16))
			calls := 0
			a.captureAction = func(string, []byte, capturePayload) (CaptureActionResult, error) {
				calls++
				if calls == 1 {
					if cancel {
						return CaptureActionResult{Cancelled: true}, nil
					}
					return CaptureActionResult{}, errors.New("disk full")
				}
				return CaptureActionResult{Completed: true}, nil
			}
			res, err := a.finishAnnotated("save", raw)
			if cancel && (err != nil || !res.Cancelled) {
				t.Fatalf("cancel result %+v %v", res, err)
			}
			if !cancel && err == nil {
				t.Fatal("action failure reported success")
			}
			if !a.capturing {
				t.Fatal("action failure discarded editable capture")
			}
			res, err = a.finishAnnotated("save", raw)
			if err != nil || !res.Completed || a.capturing {
				t.Fatalf("retry failed %+v %v", res, err)
			}
			if got := len(a.store.List()); got != 1 {
				t.Fatalf("retry created %d history records", got)
			}
		})
	}
}

func TestCaptureDoesNotReportHistoryWriteFailureAsSuccess(t *testing.T) {
	a := newTestApp(t)
	a.historyDir = a.store.Dir()
	if err := os.Remove(filepath.Join(a.historyDir, "index.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(a.historyDir, "index.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := a.storeCapture(gnPNGData(t, 24, 16)); err == nil {
		t.Fatal("history failure reported success")
	}
}

func TestPreparationCacheOnlyKeepsCurrentOverlayAndResult(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	a := newTestApp(t)
	a.sessionDir = t.TempDir()
	a.capturing = true
	a.overlayDisplay = mygo.Display{Bounds: mygo.Rectangle{Width: 1920, Height: 1080}}
	for i := 0; i < 6; i++ {
		if _, err := a.prepareSelectionWithDelay(Rect{Width: 100, Height: 100}, 0); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(a.sessionDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("cache has %d files: %v", len(files), err)
	}
	a.cancelRegion()
	files, err = os.ReadDir(a.sessionDir)
	if err != nil || len(files) != 0 {
		t.Fatalf("cancel retained %d files: %v", len(files), err)
	}
}

func TestStaleScrollActionCannotMutateNewSession(t *testing.T) {
	s := &scrollSession{id: "new", stop: make(chan struct{}), done: make(chan struct{})}
	a := &App{scroll: s, capturing: true}
	if err := a.setScrollPausedFor("old", true); err == nil {
		t.Fatal("old pause accepted")
	}
	if err := a.finishScrollFor("old", "cancel"); err == nil {
		t.Fatal("old finish accepted")
	}
	if s.isPaused() || s.isStopped() || a.scroll != s {
		t.Fatal("old action mutated new session")
	}
}

func TestScrollControlsAreHiddenDuringCaptureAndRestoredOnlyForActiveSession(t *testing.T) {
	s := &scrollSession{id: "active"}
	a := &App{scroll: s}
	hidden, shown := 0, 0
	err := a.withScrollChromeHidden(s, func() { hidden++ }, func() { shown++ }, func() error {
		if hidden != 1 || shown != 0 {
			t.Fatal("chrome was visible during capture")
		}
		return nil
	}, 0)
	if err != nil || shown != 1 {
		t.Fatalf("chrome not restored: %v %d", err, shown)
	}
	_ = a.withScrollChromeHidden(s, func() { hidden++ }, func() { shown++ }, func() error { a.scroll = &scrollSession{id: "new"}; return nil }, time.Duration(0))
	if shown != 1 {
		t.Fatal("old capture showed chrome for new session")
	}
}

func TestScrollStopsAtAccumulatorPixelCap(t *testing.T) {
	s := &scrollSession{acc: stitch.New(stitch.Options{MaxHeight: 5})}
	img, err := pngToRGBA(gnPNGData(t, 24, 5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.acc.Add(img); err != nil {
		t.Fatal(err)
	}
	s.frames, s.height = 1, 5
	if !scrollReachedLimit(s) {
		t.Fatal("accumulator cap did not stop capture below global height/frame caps")
	}
}

func TestFailedOverlayCacheRemovalDoesNotAccumulatePreviews(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	a := newTestApp(t)
	a.sessionDir = t.TempDir()
	a.capturing = true
	a.overlayDisplay = mygo.Display{Bounds: mygo.Rectangle{Width: 1920, Height: 1080}}
	a.overlaySessionFile = "blocked.png"
	blocked := filepath.Join(a.sessionDir, a.overlaySessionFile)
	if err := os.Mkdir(blocked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "keep"), []byte("do not remove"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := a.prepareSelectionWithDelay(Rect{Width: 100, Height: 100}, 0); err == nil {
			t.Fatal("failed cache removal reported success")
		}
		files, err := os.ReadDir(a.sessionDir)
		if err != nil || len(files) != 1 {
			t.Fatalf("cache grew after failed cleanup: %d files, %v", len(files), err)
		}
	}
	if a.overlaySessionFile != "blocked.png" {
		t.Fatal("forgot the file that still needs cleanup")
	}
}

func TestFailedQuitDoesNotStopRunningScroll(t *testing.T) {
	s := &scrollSession{stop: make(chan struct{})}
	a := &App{scroll: s, capturing: true}
	if err := a.prepareQuit(func() error { return errors.New("disk full") }); err == nil {
		t.Fatal("failed snapshot allowed quit")
	}
	if a.quitting || s.isStopped() || a.scroll != s || !a.capturing {
		t.Fatal("cancelled quit left the live session stopped or inaccessible")
	}
	if err := a.prepareQuit(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !a.quitting || !s.isStopped() {
		t.Fatal("successful quit did not stop capture")
	}
}

func TestOverlayCommandsCannotInterruptActiveScroll(t *testing.T) {
	a := newTestApp(t)
	s := &scrollSession{id: "scroll-1", stop: make(chan struct{})}
	a.scroll, a.capturing = s, true
	a.overlayDisplay = mygo.Display{Bounds: mygo.Rectangle{Width: 1920, Height: 1080}}
	a.cancelRegion()
	if !a.capturing || a.scroll != s {
		t.Fatal("late overlay cancellation interrupted scrolling")
	}
	if _, err := a.finishAnnotated("cancel", ""); err == nil {
		t.Fatal("ignored annotation cancel reported completed")
	}
	if _, err := a.prepareSelectionWithDelay(Rect{Width: 100, Height: 100}, 0); err == nil {
		t.Fatal("late overlay preparation accepted during scrolling")
	}
	a.captureAction = func(string, []byte, capturePayload) (CaptureActionResult, error) {
		t.Fatal("late annotation invoked an external action")
		return CaptureActionResult{}, nil
	}
	if _, err := a.finishAnnotated("copy", base64.StdEncoding.EncodeToString(gnPNGData(t, 24, 16))); err == nil {
		t.Fatal("late annotation accepted during scrolling")
	}
}

func TestOldScrollCaptureCannotHideNewSession(t *testing.T) {
	old, current := &scrollSession{id: "old"}, &scrollSession{id: "current"}
	a := &App{scroll: current}
	err := a.withScrollChromeHidden(old, func() { t.Error("old capture hid the new session") }, func() { t.Error("old capture restored chrome") }, func() error { t.Error("old session still captured pixels"); return nil }, 0)
	if err == nil {
		t.Fatal("old session capture reported success")
	}
}

func TestCancelDuringFinalizationCannotPublishScrollResult(t *testing.T) {
	s := &scrollSession{id: "current", finalized: true, stop: make(chan struct{}), done: make(chan struct{})}
	a := &App{scroll: s, capturing: true}
	finished := make(chan error, 1)
	go func() { finished <- a.finishScroll("cancel") }()
	for !s.isStopped() {
		time.Sleep(time.Millisecond)
	}
	if a.scrollResultIsCurrent(s) {
		t.Error("cancelled finalization can still publish a result")
	}
	close(s.done)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestOldResultActionsCannotOperateOnNewPreview(t *testing.T) {
	a := &App{resultPNG: []byte("new preview"), resultMeta: ScrollResultPayload{ID: "new"}}
	service := &CaptureService{app: a}
	if err := service.CopyScrollResult("old"); err == nil {
		t.Error("stale copy accepted")
	}
	if _, err := service.SaveScrollResult("old"); err == nil {
		t.Error("stale save accepted")
	}
	if _, err := service.PinScrollResult("old"); err == nil {
		t.Error("stale pin accepted")
	}
	if err := service.CloseScrollResult("old"); err == nil {
		t.Error("stale close accepted")
	}
	if _, err := a.scrollResultSnapshot(""); err == nil {
		t.Error("missing preview identity accepted")
	}
	if got, err := a.scrollResultSnapshot("new"); err != nil || string(got.png) != "new preview" {
		t.Fatalf("current preview unavailable: %s %v", got.png, err)
	}
}

func TestQuitInvalidatesDelayedFullScreenCapture(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	synctest.Test(t, func(t *testing.T) {
		a := newTestApp(t)
		a.capturing, a.captureGeneration = true, 1
		a.captureAction = func(string, []byte, capturePayload) (CaptureActionResult, error) {
			t.Error("quit capture performed external action")
			return CaptureActionResult{}, nil
		}
		done := make(chan struct{})
		go func() { a.captureFullScreenAction(capture.Rect{Width: 24, Height: 16}, false, 1); close(done) }()
		synctest.Wait()
		if err := a.prepareQuit(func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		<-done
		if len(a.store.List()) != 0 {
			t.Error("quit capture still wrote history")
		}
	})
}

func TestPreviewWriteFailurePreservesPreviousFile(t *testing.T) {
	a := &App{sessionDir: t.TempDir()}
	previous := "old.png"
	path := filepath.Join(a.sessionDir, previous)
	if err := os.WriteFile(path, []byte("previous pixels"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := a.writePreviewLockedWith(context.Background(), &previous, []byte("new pixels"), func(f *os.File, data []byte) error { _, _ = f.Write(data[:3]); return errors.New("disk full") })
	if err == nil {
		t.Fatal("failed preview write reported success")
	}
	if previous != "old.png" {
		t.Fatal("failed replacement lost previous preview ownership")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "previous pixels" {
		t.Fatalf("previous preview lost: %q %v", data, err)
	}
	if files, _ := os.ReadDir(a.sessionDir); len(files) != 1 {
		t.Fatalf("failed preview left %d files", len(files))
	}
}

func TestCanceledPreviewWriteKeepsPreviousFile(t *testing.T) {
	application := &App{sessionDir: t.TempDir()}
	previous := "previous.png"
	if err := os.WriteFile(filepath.Join(application.sessionDir, previous), []byte("old preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := application.writePreviewLockedWith(ctx, &previous, []byte("new preview"), func(file *os.File, data []byte) error {
		_, err := file.Write(data)
		cancel()
		return err
	})
	if !errors.Is(err, context.Canceled) || previous != "previous.png" {
		t.Fatalf("canceled write replaced the preview: %q, %v", previous, err)
	}
	files, err := os.ReadDir(application.sessionDir)
	if err != nil || len(files) != 1 || files[0].Name() != previous {
		t.Fatalf("canceled write leaked a preview: %v, %v", files, err)
	}
}

func TestCancelledResultPublicationKeepsPreviousPreview(t *testing.T) {
	a := newTestApp(t)
	a.sessionDir = t.TempDir()
	a.resultSessionFile, a.resultMeta.ID = "old.png", "old.png"
	a.resultPNG = []byte("previous pixels")
	if err := os.WriteFile(filepath.Join(a.sessionDir, "old.png"), a.resultPNG, 0600); err != nil {
		t.Fatal(err)
	}
	s := &scrollSession{id: "old-session"}
	a.scroll = s
	a.scrollUI = func(fn func()) { a.scroll = &scrollSession{id: "new-session"}; fn() }
	if err := a.showScrollResultFor(s, gnPNGData(t, 24, 16), 2, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(a.sessionDir, "old.png")); err != nil || string(data) != "previous pixels" {
		t.Fatalf("cancelled finalization evicted prior preview: %q %v", data, err)
	}
	if a.resultMeta.ID != "old.png" || string(a.resultPNG) != "previous pixels" {
		t.Fatal("cancelled finalization replaced prior metadata")
	}
	if files, _ := os.ReadDir(a.sessionDir); len(files) != 1 {
		t.Fatalf("cancelled finalization retained %d files", len(files))
	}
}
