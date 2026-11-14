package app

import (
	"testing"

	"github.com/echoVic/Pinstill/internal/stitch"
)

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
	a.finalizeScroll(old, "stalled")
	if a.scroll != current || !a.capturing {
		t.Fatal("an old scroll completion cleared the newer capture")
	}
}

func TestCancelledScrollDoesNotAutoFinalize(t *testing.T) {
	session := &scrollSession{stopped: true, acc: stitch.New(stitch.Options{})}
	a := &App{scroll: session, capturing: true}
	a.finalizeScroll(session, "stalled")
	if session.isFinalized() {
		t.Fatal("cancelled scrolling capture was finalized by the capture loop")
	}
}
