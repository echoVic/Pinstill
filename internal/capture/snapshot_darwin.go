//go:build darwin

package capture

import (
	"bytes"
	"context"
	"errors"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/echoVic/Pinstill/internal/imagebudget"
)

var nativeSnapshot struct {
	manager objc.ID
	grab    objc.SEL
	width   func(uintptr) int
	height  func(uintptr) int
	retain  func(uintptr) uintptr
	release func(uintptr)
	newData func(uintptr, int) uintptr
	length  func(uintptr) int
	data    func(uintptr) unsafe.Pointer
	newDest func(uintptr, uintptr, int, uintptr) uintptr
	add     func(uintptr, uintptr, uintptr)
	finish  func(uintptr) bool
	pngType uintptr
}

var initNativeSnapshot = sync.OnceValue(func() bool {
	const (
		screenCaptureKit = "/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit"
		imageIO          = "/System/Library/Frameworks/ImageIO.framework/ImageIO"
	)
	if _, err := purego.Dlopen(screenCaptureKit, purego.RTLD_LAZY|purego.RTLD_LOCAL); err != nil {
		return false
	}
	nativeSnapshot.manager = objc.ID(objc.GetClass("SCScreenshotManager"))
	nativeSnapshot.grab = objc.RegisterName("captureImageInRect:completionHandler:")
	if nativeSnapshot.manager == 0 || !objc.Send[bool](nativeSnapshot.manager, objc.RegisterName("respondsToSelector:"), nativeSnapshot.grab) {
		return false
	}
	var makeString func(uintptr, string, uint32) uintptr
	for _, framework := range []struct {
		path    string
		symbols map[string]any
	}{
		{coreGraphicsFrameworkPath, map[string]any{
			"CGImageGetWidth": &nativeSnapshot.width, "CGImageGetHeight": &nativeSnapshot.height,
		}},
		{coreFoundationFrameworkPath, map[string]any{
			"CFRetain": &nativeSnapshot.retain, "CFRelease": &nativeSnapshot.release,
			"CFDataCreateMutable": &nativeSnapshot.newData, "CFDataGetLength": &nativeSnapshot.length,
			"CFDataGetBytePtr": &nativeSnapshot.data, "CFStringCreateWithCString": &makeString,
		}},
		{imageIO, map[string]any{
			"CGImageDestinationCreateWithData": &nativeSnapshot.newDest,
			"CGImageDestinationAddImage":       &nativeSnapshot.add,
			"CGImageDestinationFinalize":       &nativeSnapshot.finish,
		}},
	} {
		lib, err := purego.Dlopen(framework.path, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			return false
		}
		for name, binding := range framework.symbols {
			symbol, err := purego.Dlsym(lib, name)
			if err != nil || symbol == 0 {
				return false
			}
			purego.RegisterFunc(binding, symbol)
		}
	}
	nativeSnapshot.pngType = makeString(0, "public.png", kCFStringEncodingUTF8)
	return nativeSnapshot.pngType != 0
})

func captureSnapshotRegion(ctx context.Context, rect Rect, opts CaptureOptions) ([]byte, error) {
	if err := waitCaptureDelay(ctx, opts); err != nil {
		return nil, err
	}
	var native func(context.Context, Rect, CaptureOptions) ([]byte, error)
	if !opts.IncludeCursor && PermissionGranted() && initNativeSnapshot() {
		native = captureNativeSnapshot
	}
	return captureSnapshotWith(ctx, rect, opts, native, captureRegion)
}

func captureSnapshotWith(ctx context.Context, rect Rect, opts CaptureOptions, native, fallback func(context.Context, Rect, CaptureOptions) ([]byte, error)) ([]byte, error) {
	if native != nil && !opts.IncludeCursor {
		if data, err := native(ctx, rect, opts); err == nil {
			return data, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return fallback(ctx, rect, opts)
}

func captureNativeSnapshot(ctx context.Context, rect Rect, opts CaptureOptions) ([]byte, error) {
	left, top, width, height := pointRect(rect)
	if err := checkScaledCapturePixels(width, height, opts.BackingScale); err != nil {
		return nil, err
	}
	bounds := cgRect{Origin: cgPoint{X: float64(left), Y: float64(top)}, Size: cgSize{Width: float64(width), Height: float64(height)}}
	grabCtx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	if err := grabCtx.Err(); err != nil {
		return nil, err
	}
	ready := make(chan uintptr, 1)
	var resultMu sync.Mutex
	defer func() {
		resultMu.Lock()
		defer resultMu.Unlock()
		cancel()
		select {
		case image := <-ready:
			if image != 0 {
				nativeSnapshot.release(image)
			}
		default:
		}
	}()
	completion := objc.NewBlock(func(_ objc.Block, image uintptr, _ objc.ID) {
		resultMu.Lock()
		defer resultMu.Unlock()
		if grabCtx.Err() != nil {
			return
		}
		if image != 0 {
			nativeSnapshot.retain(image)
		}
		ready <- image
	})
	defer completion.Release()
	nativeSnapshot.manager.Send(nativeSnapshot.grab, bounds, completion)
	select {
	case image := <-ready:
		if image == 0 {
			return nil, errors.New("capture: ScreenCaptureKit returned no image")
		}
		defer nativeSnapshot.release(image)
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if nativeSnapshot.width(image) != int(math.Round(float64(width)*opts.BackingScale)) || nativeSnapshot.height(image) != int(math.Round(float64(height)*opts.BackingScale)) {
			return nil, errors.New("capture: unexpected ScreenCaptureKit snapshot dimensions")
		}
		return encodeNativeSnapshot(ctx, image)
	case <-grabCtx.Done():
		return nil, grabCtx.Err()
	}
}

func encodeNativeSnapshot(ctx context.Context, image uintptr) ([]byte, error) {
	data := nativeSnapshot.newData(0, 0)
	if data == 0 {
		return nil, errors.New("capture: could not allocate the PNG output buffer")
	}
	defer nativeSnapshot.release(data)
	destination := nativeSnapshot.newDest(data, nativeSnapshot.pngType, 1, 0)
	if destination == 0 {
		return nil, errors.New("capture: could not open the PNG encoder")
	}
	defer nativeSnapshot.release(destination)
	nativeSnapshot.add(destination, image, 0)
	if !nativeSnapshot.finish(destination) {
		return nil, errors.New("capture: could not encode the frozen display")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	size := nativeSnapshot.length(data)
	if size <= 0 || size > imagebudget.MaxEncodedBytes {
		return nil, errors.New("capture: native PNG exceeded the encoded byte limit")
	}
	return bytes.Clone(unsafe.Slice((*byte)(nativeSnapshot.data(data)), size)), nil
}
