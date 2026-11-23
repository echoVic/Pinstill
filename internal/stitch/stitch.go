// Package stitch joins vertically scrolled screenshots into one long image.
package stitch

import (
	"bytes"
	"errors"
	"image"
	"math"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

// ErrNoFrames is returned when no frame was given.
var ErrNoFrames = errors.New("stitch: no frames")

var ErrUnaligned = errors.New("stitch: no reliable overlap")

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
		o.MinOverlap = min(24, max(2, frameHeight/2))
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

// Add appends a connected frame. ErrUnaligned leaves the last accepted frame
// in place, so returning to overlapping content can resume the capture.
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
		a.frames++
		return 0, nil
	}
	if a.merged >= a.opts.MaxHeight {
		return 0, nil
	}
	match := matchOverlap(a.prev, frame, a.opts)
	if match.overlap == 0 {
		return 0, ErrUnaligned
	}
	// A footer occupies the tail of the previous frame. Replace that tail
	// along with the newly revealed rows, so it only appears at the end.
	writeAt := a.merged - match.footer
	writeRows := min(frame.Rect.Dy()-match.nextEnd, a.opts.MaxHeight-writeAt)
	a.canvas = growCanvas(a.canvas, writeAt+writeRows, a.opts.MaxHeight)
	copyRowsFrom(a.canvas, writeAt, frame, match.nextEnd, writeRows)
	a.merged = writeAt + writeRows
	a.prev = frame
	a.overlap = match.overlap
	a.frames++
	return match.overlap, nil
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

// SameAsPrevious reports whether the viewport still shows the last accepted
// content. Fixed chrome can change while the body stays in place.
func (a *Accumulator) SameAsPrevious(frame *image.RGBA) bool {
	if a.prev == nil || frame == nil || a.prev.Bounds().Size() != frame.Bounds().Size() {
		return false
	}
	if validateFrame(frame) != nil {
		return false
	}
	height, width := frame.Rect.Dy(), frame.Rect.Dx()
	edge := height / 5
	changedRowsLimit := max(1, (height-2*edge)/8)
	allowedDifference := float64(width*height*3) * a.opts.SameFrameDelta
	var difference, changedRows int
	for row := edge; row < height-edge; row++ {
		before := a.prev.PixOffset(a.prev.Rect.Min.X, a.prev.Rect.Min.Y+row)
		after := frame.PixOffset(frame.Rect.Min.X, frame.Rect.Min.Y+row)
		if bytes.Equal(a.prev.Pix[before:before+width*4], frame.Pix[after:after+width*4]) {
			continue
		}
		var strongest int
		for column := 0; column < width; column++ {
			priorPixel, nextPixel := before+column*4, after+column*4
			pixelDiff := absInt(int(a.prev.Pix[priorPixel])-int(frame.Pix[nextPixel])) +
				absInt(int(a.prev.Pix[priorPixel+1])-int(frame.Pix[nextPixel+1])) +
				absInt(int(a.prev.Pix[priorPixel+2])-int(frame.Pix[nextPixel+2]))
			difference += pixelDiff
			strongest = max(strongest, pixelDiff)
		}
		if strongest >= 48 {
			changedRows++
		}
		if float64(difference) >= allowedDifference || changedRows > changedRowsLimit {
			return false
		}
	}
	return true
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

func meanAbsDiffAt(prev, next *image.RGBA, prevTop, nextTop, n int, stopAt float64) float64 {
	if n <= 0 {
		return 255
	}
	b := prev.Bounds()
	if next.Bounds().Size() != b.Size() || prevTop < 0 || nextTop < 0 || prevTop+n > b.Dy() || nextTop+n > b.Dy() {
		return 255
	}
	w := b.Dx()
	samples := float64(w) * float64(n) * 3
	var total float64
	for y := 0; y < n; y++ {
		prevOff := prev.PixOffset(b.Min.X, b.Min.Y+prevTop+y)
		nextOff := next.PixOffset(next.Rect.Min.X, next.Rect.Min.Y+nextTop+y)
		for x := 0; x < w; x++ {
			pi := prevOff + x*4
			ni := nextOff + x*4
			total += float64(absInt(int(prev.Pix[pi])-int(next.Pix[ni])) +
				absInt(int(prev.Pix[pi+1])-int(next.Pix[ni+1])) +
				absInt(int(prev.Pix[pi+2])-int(next.Pix[ni+2])))
		}
		if stopAt > 0 && total/samples > stopAt {
			return total / samples
		}
	}
	return total / samples
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
