//go:build darwin

package capture

import (
	"context"
	"errors"
	"testing"
)

func TestSnapshotFallsBackWhenNativeCaptureFails(t *testing.T) {
	for _, includeCursor := range []bool{false, true} {
		nativeCalls, fallbackCalls := 0, 0
		native := func(context.Context, Rect, CaptureOptions) ([]byte, error) {
			nativeCalls++
			return nil, errors.New("native capture unavailable")
		}
		fallback := func(context.Context, Rect, CaptureOptions) ([]byte, error) {
			fallbackCalls++
			return []byte("fallback"), nil
		}
		result, err := captureSnapshotWith(context.Background(), Rect{Width: 10, Height: 10}, CaptureOptions{IncludeCursor: includeCursor}, native, fallback)
		if err != nil || string(result) != "fallback" || fallbackCalls != 1 {
			t.Fatalf("cursor=%v: result %q, error %v, fallback calls %d", includeCursor, result, err, fallbackCalls)
		}
		if nativeCalls != map[bool]int{false: 1, true: 0}[includeCursor] {
			t.Fatalf("cursor=%v: native calls %d", includeCursor, nativeCalls)
		}
	}
}

func TestSnapshotCancellationDoesNotStartFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	native := func(context.Context, Rect, CaptureOptions) ([]byte, error) {
		cancel()
		return nil, context.Canceled
	}
	fallback := func(context.Context, Rect, CaptureOptions) ([]byte, error) {
		t.Fatal("canceled native capture started the fallback")
		return nil, nil
	}
	if _, err := captureSnapshotWith(ctx, Rect{Width: 10, Height: 10}, CaptureOptions{}, native, fallback); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want canceled", err)
	}
}
