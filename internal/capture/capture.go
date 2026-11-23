package capture

import (
	"context"
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

func CaptureRegion(rect Rect, opts CaptureOptions) ([]byte, error) {
	return CaptureRegionContext(context.Background(), rect, opts)
}

func CaptureRegionContext(ctx context.Context, rect Rect, opts CaptureOptions) ([]byte, error) {
	if err := waitCaptureDelay(ctx, opts); err != nil {
		return nil, err
	}
	return captureRegion(ctx, rect, opts)
}

func waitCaptureDelay(ctx context.Context, opts CaptureOptions) error {
	if opts.DelayMillis > 0 {
		timer := time.NewTimer(time.Duration(min(opts.DelayMillis, 10_000)) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}
