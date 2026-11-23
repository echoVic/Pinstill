package capture

import (
	"errors"
	"time"
)

// ErrUnsupported is returned by platforms that have no real capture backend.
var ErrUnsupported = errors.New("capture: unsupported on this platform")

// CaptureOptions controls how a region capture is performed.
type CaptureOptions struct {
	// IncludeCursor draws the mouse cursor into the captured image.
	IncludeCursor bool
	// DelayMillis waits before the screen is grabbed so transient UI (such as
	// the selection overlay) can disappear first. Zero or negative means no
	// extra delay. Delays are capped at ten seconds.
	DelayMillis  int
	BackingScale float64
}

// sleepBeforeCapture blocks for opts.DelayMillis when a delay was requested.
func sleepBeforeCapture(opts CaptureOptions) {
	if opts.DelayMillis > 0 {
		time.Sleep(time.Duration(min(opts.DelayMillis, 10_000)) * time.Millisecond)
	}
}
