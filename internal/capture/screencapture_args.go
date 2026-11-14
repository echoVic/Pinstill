package capture

import "fmt"

// buildScreencaptureArgs assembles the argument vector for macOS'
// /usr/sbin/screencapture to capture a rectangle. It is a pure function so the
// command line contract can be unit tested on every platform; the caller adds
// the output file path as the final argument.
//
// -x disables the shutter sound, -t png forces PNG output, -C includes the
// cursor, and -R takes x,y,w,h in screen points (DIP). The OS scales the
// output to device pixels on Retina displays.
func buildScreencaptureArgs(rect Rect, opts CaptureOptions) []string {
	args := []string{"-x", "-t", "png"}
	if opts.IncludeCursor {
		args = append(args, "-C")
	}
	x, y, width, height := pointRect(rect)
	return append(args, "-R", fmt.Sprintf("%d,%d,%d,%d", x, y, width, height))
}

// pointRect rounds a logical rectangle to the integer point bounds understood
// by screencapture's -R flag. Width and height never drop to zero for a
// non-empty rectangle.
func pointRect(rect Rect) (x, y, width, height int) {
	return PixelRect(rect, 1)
}
