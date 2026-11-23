//go:build darwin

package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/echoVic/Pinstill/internal/imagebudget"
)

const (
	// screencapturePath is the system utility used to grab the screen. It is
	// always present on macOS; no cgo and no private APIs are needed.
	screencapturePath = "/usr/sbin/screencapture"
	// openPath launches System Settings with a specific pane.
	openPath = "/usr/bin/open"
	// screenRecordingSettingsURL points at Privacy & Security > Screen
	// Recording (macOS 13+ keeps the pane identifier stable).
	screenRecordingSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	// coreGraphicsFrameworkPath is where CGPreflightScreenCaptureAccess lives.
	coreGraphicsFrameworkPath = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
)

// Supported reports whether this platform has a real screen capture backend.
func Supported() bool { return true }

// captureRegion captures rect (screen points, origin at the primary display's
// top-left, negative values allowed) and returns the PNG encoded result. The
// system utility writes Retina screenshots at device pixel resolution.
func captureRegion(ctx context.Context, rect Rect, opts CaptureOptions) ([]byte, error) {
	_, _, width, height := pointRect(rect)
	if width == 0 || height == 0 {
		return nil, errors.New("capture: empty or invalid capture region")
	}
	if err := imagebudget.CheckDimensions(width, height); err != nil {
		return nil, err
	}
	if err := checkDarwinCapturePixels(width, height, opts.BackingScale); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "pinstill-capture-*.png")
	if err != nil {
		return nil, fmt.Errorf("capture: create temp file: %w", err)
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return nil, fmt.Errorf("capture: close temp file: %w", err)
	}
	defer func() { _ = os.Remove(name) }()

	args := append(buildScreencaptureArgs(rect, opts), name)
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	stderr, err := runCaptureCommand(ctx, screencapturePath, args...)
	if err != nil {
		if detail := strings.TrimSpace(string(stderr)); detail != "" {
			return nil, fmt.Errorf("capture: screencapture failed: %w (%s)", err, detail)
		}
		return nil, fmt.Errorf("capture: screencapture failed: %w", err)
	}

	data, err := readCaptureFile(name)
	if err != nil {
		return nil, fmt.Errorf("capture: read screenshot: %w", err)
	}
	return data, ctx.Err()
}

var (
	preflightOnce sync.Once
	// preflightFn is nil when the CoreGraphics symbol could not be resolved.
	preflightFn func() bool
)

// PermissionGranted reports whether macOS has granted screen recording access
// to this process. The check goes through CoreGraphics'
// CGPreflightScreenCaptureAccess, which never prompts the user. If the symbol
// cannot be resolved (for example on an older macOS release) it returns true
// and leaves permission failures to CaptureRegion.
func PermissionGranted() bool {
	preflightOnce.Do(func() {
		lib, err := purego.Dlopen(coreGraphicsFrameworkPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			return
		}
		symbol, err := purego.Dlsym(lib, "CGPreflightScreenCaptureAccess")
		if err != nil || symbol == 0 {
			return
		}
		var fn func() bool
		purego.RegisterFunc(&fn, symbol)
		preflightFn = fn
	})
	if preflightFn == nil {
		return true
	}
	return preflightFn()
}

var (
	requestOnce sync.Once
	requestFn   func() bool
)

// RequestPermission asks macOS for screen recording access. When the user has
// not answered before, macOS shows the system prompt for this app. It returns
// whether access is granted afterwards.
func RequestPermission() bool {
	if PermissionGranted() {
		return true
	}
	requestOnce.Do(func() {
		lib, err := purego.Dlopen(coreGraphicsFrameworkPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			return
		}
		symbol, err := purego.Dlsym(lib, "CGRequestScreenCaptureAccess")
		if err != nil || symbol == 0 {
			return
		}
		var fn func() bool
		purego.RegisterFunc(&fn, symbol)
		requestFn = fn
	})
	if requestFn == nil {
		return false
	}
	return requestFn()
}

// OpenPermissionSettings opens System Settings > Privacy & Security > Screen
// Recording so the user can grant access.
func OpenPermissionSettings() error {
	if err := exec.Command(openPath, screenRecordingSettingsURL).Run(); err != nil {
		return fmt.Errorf("capture: open screen recording settings: %w", err)
	}
	return nil
}
