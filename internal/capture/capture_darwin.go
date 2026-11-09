//go:build darwin

package capture

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
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

// CaptureRegion captures rect (screen points, origin at the primary display's
// top-left, negative values allowed) and returns the PNG encoded result. The
// system utility writes Retina screenshots at device pixel resolution.
func CaptureRegion(rect Rect, opts CaptureOptions) ([]byte, error) {
	if rect.Empty() {
		return nil, errors.New("capture: empty capture region")
	}
	sleepBeforeCapture(opts)

	tmp, err := os.CreateTemp("", "tupen-capture-*.png")
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
	cmd := exec.Command(screencapturePath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("capture: screencapture failed: %w (%s)", err, detail)
		}
		return nil, fmt.Errorf("capture: screencapture failed: %w", err)
	}

	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("capture: read screenshot: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("capture: screencapture produced an empty file")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("capture: screencapture produced invalid PNG: %w", err)
	}
	return data, nil
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

// OpenPermissionSettings opens System Settings > Privacy & Security > Screen
// Recording so the user can grant access.
func OpenPermissionSettings() error {
	if err := exec.Command(openPath, screenRecordingSettingsURL).Run(); err != nil {
		return fmt.Errorf("capture: open screen recording settings: %w", err)
	}
	return nil
}
