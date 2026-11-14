// Package stitch joins vertically scrolled screenshots into one long image.
package stitch

import (
	"errors"
	"image"
	"math"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

// ErrNoFrames is returned when no frame was given.
var ErrNoFrames = errors.New("stitch: no frames")

// Options configures stitching.
type Options struct {
	// MinOverlap is the smallest overlap height searched, in pixels.
	MinOverlap int
	// MaxOverlap caps the overlap search. Zero means frame height minus one,
	// including small scroll movements.
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
		o.MaxOverlap = frameHeight - 1
	}
	if o.MaxOverlap > frameHeight-1 {
		o.MaxOverlap = frameHeight - 1
	}
	if o.Tolerance <= 0 || math.IsNaN(o.Tolerance) || math.IsInf(o.Tolerance, 0) {
		o.Tolerance = 6
	}
	if o.MaxHeight <= 0 {
		o.MaxHeight = 30000
	}
	if o.SameFrameDelta <= 0 || math.IsNaN(o.SameFrameDelta) || math.IsInf(o.SameFrameDelta, 0) {
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
	opts    Options
	canvas  *image.RGBA
	merged  int // rows written into canvas
	frames  int
	prev    *image.RGBA
	overlap int // overlap used for the last frame that appended rows
}

// New returns an empty accumulator. The first frame sets the width.
func New(opts Options) *Accumulator { return &Accumulator{opts: opts} }

// Add appends one frame and returns the overlap height used (0 for the first
// frame or when no confident overlap was found).
func (a *Accumulator) Add(frame *image.RGBA) (int, error) {
	if frame == nil || frame.Bounds().Empty() {
		return 0, errors.New("stitch: empty frame")
	}
	if err := validateFrame(frame); err != nil {
		return 0, err
	}
	if a.prev != nil && a.prev.Bounds().Size() != frame.Bounds().Size() {
		return 0, errors.New("stitch: frame dimensions changed")
	}
	a.frames++
	if a.canvas == nil {
		a.opts = a.opts.withDefaults(frame.Bounds().Dy())
		b := frame.Bounds()
		a.opts.MaxHeight = min(a.opts.MaxHeight, imagebudget.MaxPixels/b.Dx())
		// The height cap applies to the first frame too: keep a.merged in sync
		// with the canvas, otherwise Size()/Result() would report more rows
		// than the image holds when one frame is taller than MaxHeight.
		a.canvas = image.NewRGBA(image.Rect(0, 0, b.Dx(), min(a.opts.MaxHeight, b.Dy())))
		a.merged = a.canvas.Bounds().Dy()
		copyRows(a.canvas, 0, frame, a.merged)
		a.prev = frame
		return 0, nil
	}
	if a.merged >= a.opts.MaxHeight {
		a.prev = frame
		return 0, nil
	}
	match := findOverlapWithPrefix(a.prev, frame, a.opts)
	overlap := match.overlap
	if overlap <= 0 {
		// No confident overlap: append the whole frame rather than lose
		// content (a best-effort long screenshot).
		overlap = 0
	}
	srcTop := match.prefix + overlap
	if frame.Bounds().Dy()-srcTop > a.opts.MaxHeight-a.merged {
		a.canvas = growCanvas(a.canvas, a.opts.MaxHeight, a.opts.MaxHeight)
		appended := a.opts.MaxHeight - a.merged
		if appended > 0 {
			copyRowsFrom(a.canvas, a.merged, frame, srcTop, appended)
			a.merged += appended
		}
		a.prev = frame
		a.overlap = overlap
		return overlap, nil
	}
	appendRows := frame.Bounds().Dy() - srcTop
	a.canvas = growCanvas(a.canvas, a.merged+appendRows, a.opts.MaxHeight)
	copyRowsFrom(a.canvas, a.merged, frame, srcTop, appendRows)
	a.merged += appendRows
	a.prev = frame
	a.overlap = overlap
	return overlap, nil
}

// Result returns the stitched image so far.
func (a *Accumulator) Result() Result {
	res := Result{Frames: a.frames, Overlap: a.overlap}
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
	if a.prev == nil || frame == nil || a.prev.Bounds().Size() != frame.Bounds().Size() {
		return false
	}
	if validateFrame(frame) != nil {
		return false
	}
	return meanAbsDiff(a.prev, frame, 0, frame.Bounds().Dy(), 1, a.opts.SameFrameDelta) < a.opts.SameFrameDelta
}

func validateFrame(frame *image.RGBA) error {
	w, h := frame.Rect.Dx(), frame.Rect.Dy()
	if err := imagebudget.CheckDimensions(w, h); err != nil {
		return err
	}
	rowBytes := w * 4
	if frame.Stride < rowBytes || len(frame.Pix) < rowBytes || h-1 > (len(frame.Pix)-rowBytes)/frame.Stride {
		return errors.New("stitch: invalid RGBA pixel storage")
	}
	return nil
}

func growCanvas(src *image.RGBA, height, maxHeight int) *image.RGBA {
	if src.Bounds().Dy() >= height {
		return src
	}
	// Grow geometrically so small scroll steps do not repeatedly allocate and
	// copy the entire accumulated image. Spare rows stay below the height cap
	// and Result exposes only rows that have actually been written.
	current := src.Bounds().Dy()
	height = max(height, current+min(current, maxHeight-current))
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
		srcOff := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+srcTop+y)
		dstOff := dst.PixOffset(dst.Rect.Min.X, dst.Rect.Min.Y+dstTop+y)
		copy(dst.Pix[dstOff:dstOff+w*4], src.Pix[srcOff:srcOff+w*4])
	}
}

// findOverlap searches every permitted offset, with linear row-fingerprint
// matching for unchanged content. Noisy matches use a bounded shortlist;
// every accepted candidate is verified against every RGB pixel. Work is
// O(width*height + height*probeRows*signatureSize + candidates*width*height),
// rather than comparing all pixels for all overlap heights.
type overlapMatch struct {
	prefix  int
	overlap int
}

func findOverlap(prev, next *image.RGBA, opts Options) int {
	return findOverlapWithPrefix(prev, next, opts).overlap
}

func findOverlapWithPrefix(prev, next *image.RGBA, opts Options) overlapMatch {
	h := prev.Bounds().Dy()
	if h != next.Bounds().Dy() {
		return overlapMatch{}
	}
	prevRows, nextRows := fingerprintRows(prev), fingerprintRows(next)
	for _, prefix := range []int{commonPrefixRows(prevRows, nextRows, h/3), 0} {
		bodyRows := len(nextRows) - prefix
		maxOverlap := min(opts.MaxOverlap, min(h-1, bodyRows-1))
		if maxOverlap < opts.MinOverlap {
			continue
		}
		body := nextRows[prefix:]
		if overlap := exactOverlap(prevRows, body, opts.MinOverlap, maxOverlap); overlap > 0 {
			if meanAbsDiffAt(prev, next, h-overlap, prefix, overlap, 1, opts.Tolerance) == 0 {
				return overlapMatch{prefix: prefix, overlap: overlap}
			}
		}
		if overlap := noisyOverlap(prev, next, opts, maxOverlap, prefix); overlap > 0 {
			return overlapMatch{prefix: prefix, overlap: overlap}
		}
	}
	return overlapMatch{}
}

func commonPrefixRows(prev, next []uint64, max int) int {
	if max < 2 {
		return 0
	}
	if max > len(prev) || max > len(next) {
		max = min(len(prev), len(next))
	}
	n := 0
	for n < max && prev[n] == next[n] {
		n++
	}
	if n < 2 {
		return 0
	}
	return n
}

// meanAbsDiff compares n rows between two images: rows of prev starting at
// prevTop against rows of next starting at 0. step samples every step-th
// column. It gives up early only when the accumulated difference would exceed
// stopAt even if all remaining samples matched exactly.
func meanAbsDiff(prev, next *image.RGBA, prevTop, n, step int, stopAt float64) float64 {
	return meanAbsDiffAt(prev, next, prevTop, 0, n, step, stopAt)
}

func meanAbsDiffAt(prev, next *image.RGBA, prevTop, nextTop, n, step int, stopAt float64) float64 {
	if n <= 0 {
		return 255
	}
	if step < 1 {
		step = 1
	}
	b := prev.Bounds()
	if next.Bounds().Size() != b.Size() || prevTop < 0 || nextTop < 0 || prevTop+n > b.Dy() || nextTop+n > b.Dy() {
		return 255
	}
	w := b.Dx()
	samples := float64((w-1)/step+1) * float64(n) * 3
	var total float64
	var count float64
	for y := 0; y < n; y++ {
		prevOff := prev.PixOffset(b.Min.X, b.Min.Y+prevTop+y)
		nextOff := next.PixOffset(next.Rect.Min.X, next.Rect.Min.Y+nextTop+y)
		for x := 0; x < w; x += step {
			pi := prevOff + x*4
			ni := nextOff + x*4
			total += float64(absInt(int(prev.Pix[pi])-int(next.Pix[ni])) +
				absInt(int(prev.Pix[pi+1])-int(next.Pix[ni+1])) +
				absInt(int(prev.Pix[pi+2])-int(next.Pix[ni+2])))
			count += 3
		}
		if stopAt > 0 && total/samples > stopAt {
			return total / samples
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
