//go:build !darwin

package capture

import "context"

func captureSnapshotRegion(ctx context.Context, rect Rect, opts CaptureOptions) ([]byte, error) {
	return CaptureRegionContext(ctx, rect, opts)
}
