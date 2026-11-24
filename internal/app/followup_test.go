package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		previous := []byte("previous preview")
		application := &App{
			capturing:         true,
			settings:          DefaultSettings(),
			overlayDisplay:    mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}},
			overlayPreviewID:  "previous",
			overlayPreviewPNG: previous,
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
		if application.overlayPreviewID != "previous" || !bytes.Equal(application.overlayPreviewPNG, previous) {
			t.Fatal("canceled preparation replaced the previous preview")
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

func TestSelectionPreviewSurvivesDeletedDiskCache(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	dataDir := t.TempDir()
	t.Setenv("PINSTILL_DATA_DIR", dataDir)
	legacyCache := filepath.Join(dataDir, "session")
	if err := os.Mkdir(legacyCache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyCache, "old.png"), []byte("previous preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := New()
	if err := application.initStorage(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacyCache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old preview cache was not removed at startup: %v", err)
	}
	if err := os.Mkdir(legacyCache, 0o755); err != nil {
		t.Fatal(err)
	}
	application.capturing = true
	application.overlayDisplay = mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}
	handler := application.shotHandler()
	prepare := func() OverlayCapturePayload {
		t.Helper()
		preview, err := application.prepareSelectionWithDelay(Rect{Width: 80, Height: 50}, 0)
		if err != nil {
			t.Fatal(err)
		}
		return preview
	}
	first := prepare()
	if err := os.RemoveAll(legacyCache); err != nil {
		t.Fatal(err)
	}
	assertPNGPreview(t, handler, first, http.StatusOK)
	second := prepare()
	assertPNGPreview(t, handler, first, http.StatusNotFound)
	assertPNGPreview(t, handler, second, http.StatusOK)
	if _, err := os.Lstat(legacyCache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview recreated the old cache directory: %v", err)
	}
	if err := os.WriteFile(legacyCache, []byte("blocked directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	third := prepare()
	assertPNGPreview(t, handler, third, http.StatusOK)
	application.cancelRegion()
	assertPNGPreview(t, handler, third, http.StatusNotFound)
}

func TestScrollResultPreviewSurvivesDeletedDiskCache(t *testing.T) {
	legacyCache := filepath.Join(t.TempDir(), "session")
	if err := os.Mkdir(legacyCache, 0o755); err != nil {
		t.Fatal(err)
	}
	image := gnPNGData(t, 24, 48)
	application := &App{resultPNG: image, resultMeta: ScrollResultPayload{ID: "result.png", Width: 24, Height: 48}}
	preview := OverlayCapturePayload{ID: "result", URL: "shot://localhost/s/result.png", Width: 24, Height: 48}
	handler := application.shotHandler()
	if err := os.RemoveAll(legacyCache); err != nil {
		t.Fatal(err)
	}
	assertPNGPreview(t, handler, preview, http.StatusOK)
	result, err := application.scrollResultSnapshot("result.png")
	if err != nil || !bytes.Equal(result.png, image) {
		t.Fatalf("scroll actions lost their image: %v", err)
	}
	application.mu.Lock()
	application.resultPNG = nil
	application.resultMeta = ScrollResultPayload{}
	application.mu.Unlock()
	assertPNGPreview(t, handler, preview, http.StatusNotFound)
}

func TestCaptureActionCompletesAfterPreviewCacheDeletion(t *testing.T) {
	t.Setenv("PINSTILL_SELFTEST_FAKE_CAPTURE", "1")
	application := newTestApp(t)
	application.capturing = true
	application.overlayDisplay = mygo.Display{Bounds: mygo.Rectangle{Width: 800, Height: 600}}
	preview, err := application.prepareSelectionWithDelay(Rect{Width: 80, Height: 50}, 0)
	if err != nil {
		t.Fatal(err)
	}
	legacyCache := filepath.Join(t.TempDir(), "session")
	if err := os.Mkdir(legacyCache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(legacyCache); err != nil {
		t.Fatal(err)
	}
	image := application.overlayPreviewPNG
	application.captureAction = func(action string, data []byte, payload capturePayload) (CaptureActionResult, error) {
		if action != "copy" || !bytes.Equal(data, image) || payload.HistoryID == "" {
			t.Fatalf("unexpected capture action: %q, %+v", action, payload)
		}
		return CaptureActionResult{Completed: true}, nil
	}
	result, err := application.finishAnnotated("copy", base64.StdEncoding.EncodeToString(image))
	if err != nil || !result.Completed {
		t.Fatalf("capture action failed: %+v, %v", result, err)
	}
	assertPNGPreview(t, application.shotHandler(), preview, http.StatusNotFound)
	if len(application.store.List()) != 1 {
		t.Fatal("completed capture was not saved to history")
	}
}

func assertPNGPreview(t *testing.T, handler http.Handler, preview OverlayCapturePayload, wantStatus int) {
	t.Helper()
	previewURL, err := url.Parse(preview.URL)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, previewURL.Path, nil))
	if response.Code != wantStatus {
		t.Fatalf("preview %s returned %d, want %d", preview.ID, response.Code, wantStatus)
	}
	if wantStatus == http.StatusOK {
		if contentType, cacheControl := response.Header().Get("Content-Type"), response.Header().Get("Cache-Control"); contentType != "image/png" || cacheControl != "no-store" {
			t.Fatalf("preview headers: content type %q, cache control %q", contentType, cacheControl)
		}
		if width, height := imageDimensions(response.Body.Bytes()); width != preview.Width || height != preview.Height {
			t.Fatalf("preview %s decoded as %dx%d, want %dx%d", preview.ID, width, height, preview.Width, preview.Height)
		}
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

func TestCancelledResultPublicationKeepsPreviousPreview(t *testing.T) {
	a := newTestApp(t)
	a.resultMeta = ScrollResultPayload{ID: "old.png", Width: 16, Height: 24}
	previous := gnPNGData(t, 16, 24)
	a.resultPNG = previous
	s := &scrollSession{id: "old-session"}
	a.scroll = s
	a.scrollUI = func(fn func()) { a.scroll = &scrollSession{id: "new-session"}; fn() }
	if err := a.showScrollResultFor(s, gnPNGData(t, 24, 16), 2, false); err != nil {
		t.Fatal(err)
	}
	result, err := a.scrollResultSnapshot("old.png")
	if err != nil || !bytes.Equal(result.png, previous) {
		t.Fatalf("cancelled finalization replaced the active result: %v", err)
	}
	response := httptest.NewRecorder()
	a.shotHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/s/old.png", nil))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), previous) {
		t.Fatalf("cancelled finalization evicted the preview: status %d", response.Code)
	}
}
