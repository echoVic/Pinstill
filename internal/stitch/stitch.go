// Package stitch joins vertically scrolled screenshots into one long image.
package stitch

import (
	"errors"
	"image"
)

// ErrNoFrames is returned when no frame was given.
var ErrNoFrames = errors.New("stitch: no frames")

// Options configures stitching.
type Options struct {
	// MinOverlap is the smallest overlap height searched, in pixels.
	MinOverlap int
	// MaxOverlap caps the overlap search. Zero means 80% of the frame height.
	MaxOverlap int
	// Tolerance is the mean absolute difference (0..255) under which an
	// overlap counts as a match. Zero means 6.
	Tolerance float64
	// MaxHeight stops appending beyond this many pixels (0 = 30000).
	MaxHeight int
	// SameFrameDelta is the mean absolute difference below which two
	// consecutive frames count as identical (0 = 0.6), meaning scrolling
	// reached the end or paused.
	SameFrameDelta float64
}

func (o Options) withDefaults(frameHeight int) Options {
	if o.MinOverlap <= 0 {
		o.MinOverlap = 24
	}
	if o.MaxOverlap <= 0 {
		o.MaxOverlap = frameHeight * 4 / 5
	}
	if o.MaxOverlap > frameHeight-1 {
		o.MaxOverlap = frameHeight - 1
	}
	if o.Tolerance <= 0 {
		o.Tolerance = 6
	}
	if o.MaxHeight <= 0 {
		o.MaxHeight = 30000
	}
	if o.SameFrameDelta <= 0 {
		o.SameFrameDelta = 0.6
	}
	return o
}

// Result is the accumulated stitched image.
type Result struct {
	Image  *image.RGBA
	Frames int
	// Overlap is the overlap height found for the last appended frame.
	Overlap int
	// ReachedMax reports the height cap stopped further appending.
	ReachedMax bool
}

// Accumulator stitches frames one by one.
type Accumulator struct {
	opts   Options
	canvas *image.RGBA
	merged int // rows written into canvas
	frames int
	prev   *image.RGBA
}

// New returns an empty accumulator. The first frame sets the width.
func New(opts Options) *Accumulator { return &Accumulator{opts: opts} }

// Add appends one frame and returns the overlap height used (0 for the first
// frame or when no confident overlap was found).
func (a *Accumulator) Add(frame *image.RGBA) (int, error) {
	if frame == nil || frame.Bounds().Empty() {
		return 0, errors.New("stitch: empty frame")
	}
	a.frames++
	if a.canvas == nil {
		a.opts = a.opts.withDefaults(frame.Bounds().Dy())
		b := frame.Bounds()
		a.canvas = image.NewRGBA(image.Rect(0, 0, b.Dx(), min(a.opts.MaxHeight, b.Dy())))
		copyRows(a.canvas, 0, frame, b.Dy())
		a.merged = b.Dy()
		a.prev = frame
		return 0, nil
	}
	prev := a.prev
	if prev == nil || prev.Bounds() != frame.Bounds() {
		a.prev = frame
		return 0, nil
	}
	overlap := findOverlap(prev, frame, a.opts)
	if overlap <= 0 {
		// No confident overlap: append the whole frame rather than lose
		// content (a best-effort long screenshot).
		overlap = 0
	}
	if a.merged+frame.Bounds().Dy()-overlap > a.opts.MaxHeight {
		a.canvas = growCanvas(a.canvas, a.opts.MaxHeight)
		if a.merged >= a.opts.MaxHeight {
			a.prev = frame
			return 0, nil
		}
		appended := a.opts.MaxHeight - a.merged
		if appended > 0 {
			copyRowsFrom(a.canvas, a.merged, frame, overlap, appended)
			a.merged += appended
		}
		a.prev = frame
		return overlap, nil
	}
	appendRows := frame.Bounds().Dy() - overlap
	a.canvas = growCanvas(a.canvas, a.merged+appendRows)
	copyRowsFrom(a.canvas, a.merged, frame, overlap, appendRows)
	a.merged += appendRows
	a.prev = frame
	return overlap, nil
}

// Result returns the stitched image so far.
func (a *Accumulator) Result() Result {
	res := Result{Frames: a.frames}
	if a.canvas == nil {
		return res
	}
	b := a.canvas.Bounds()
	res.Image = a.canvas.SubImage(image.Rect(0, 0, b.Dx(), a.merged)).(*image.RGBA)
	res.ReachedMax = a.merged >= a.opts.MaxHeight
	return res
}

// Size returns the accumulated height and frame count.
func (a *Accumulator) Size() (height, frames int) {
	return a.merged, a.frames
}

// SameAsPrevious reports whether frame is nearly identical to the previous
// one, which means scrolling stopped or reached the end.
func (a *Accumulator) SameAsPrevious(frame *image.RGBA) bool {
	if a.prev == nil || frame == nil || a.prev.Bounds() != frame.Bounds() {
		return false
	}
	return meanAbsDiff(a.prev, frame, 0, frame.Bounds().Dy(), 4, a.opts.SameFrameDelta) < a.opts.SameFrameDelta
}

func growCanvas(src *image.RGBA, height int) *image.RGBA {
	if src.Bounds().Dy() >= height {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), height))
	copyRows(dst, 0, src, src.Bounds().Dy())
	return dst
}

// copyRows copies n rows from src into dst starting at dstTop.
func copyRows(dst *image.RGBA, dstTop int, src *image.RGBA, n int) {
	copyRowsFrom(dst, dstTop, src, 0, n)
}

// copyRowsFrom copies n rows from src starting at srcTop into dst starting
// at dstTop.
func copyRowsFrom(dst *image.RGBA, dstTop int, src *image.RGBA, srcTop, n int) {
	if n <= 0 {
		return
	}
	sw := src.Bounds().Dx()
	dw := dst.Bounds().Dx()
	w := min(sw, dw)
	rows := min(n, src.Bounds().Dy()-srcTop, dst.Bounds().Dy()-dstTop)
	for y := 0; y < rows; y++ {
		srcOff := src.PixOffset(0, srcTop+y)
		dstOff := dst.PixOffset(0, dstTop+y)
		copy(dst.Pix[dstOff:dstOff+w*4], src.Pix[srcOff:srcOff+w*4])
	}
}

// findOverlap searches the vertical overlap between the bottom of prev and
// the top of next. It tries the largest overlaps first: for a normal scroll
// the overlap is large, and this avoids picking an accidental small match in
// repetitive content.
func findOverlap(prev, next *image.RGBA, opts Options) int {
	b := prev.Bounds()
	h := b.Dy()
	maxOverlap := min(opts.MaxOverlap, h-1)
	best := -1
	bestDiff := opts.Tolerance + 1
	for overlap := maxOverlap; overlap >= opts.MinOverlap; overlap-- {
		diff := meanAbsDiff(prev, next, h-overlap, overlap, 4, opts.Tolerance)
		if diff < bestDiff {
			bestDiff = diff
			best = overlap
			if diff <= 0.01 {
				break
			}
		}
	}
	if best >= 0 && bestDiff <= opts.Tolerance {
		return best
	}
	return 0
}

// meanAbsDiff compares n rows between two images: rows of prev starting at
// prevTop against rows of next starting at 0. step samples every step-th
// column. It gives up early once the running mean clearly exceeds stopAt, so
// the overlap search stays fast for long frames.
func meanAbsDiff(prev, next *image.RGBA, prevTop, n, step int, stopAt float64) float64 {
	if n <= 0 {
		return 255
	}
	if step < 1 {
		step = 1
	}
	b := prev.Bounds()
	if next.Bounds() != b || prevTop < 0 || prevTop+n > b.Dy() {
		return 255
	}
	w := b.Dx()
	var total float64
	var count float64
	for y := 0; y < n; y++ {
		prevOff := prev.PixOffset(0, prevTop+y)
		nextOff := next.PixOffset(0, y)
		for x := 0; x < w; x += step {
			pi := prevOff + x*4
			ni := nextOff + x*4
			total += float64(absInt(int(prev.Pix[pi])-int(next.Pix[ni])) +
				absInt(int(prev.Pix[pi+1])-int(next.Pix[ni+1])) +
				absInt(int(prev.Pix[pi+2])-int(next.Pix[ni+2])))
			count += 3
		}
		if stopAt > 0 && count > 300 && total/count > stopAt*2 {
			return total / count
		}
	}
	if count == 0 {
		return 255
	}
	return total / count
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
