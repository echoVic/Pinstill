package stitch

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

func TestStitchSmallScroll(t *testing.T) {
	source := tallImage(37, 210)
	acc := New(Options{})
	if _, err := acc.Add(frame(source, 0, 200)); err != nil {
		t.Fatal(err)
	}
	if overlap, err := acc.Add(frame(source, 3, 200)); err != nil || overlap != 197 {
		t.Fatalf("small scroll overlap = %d, %v; want 197", overlap, err)
	}
	if got := acc.Result().Image; !bytes.Equal(got.Pix[:got.Stride*203], frame(source, 0, 203).Pix) {
		t.Fatal("small scroll pixels do not reconstruct source")
	}
}

func TestSameFrameSeesUnsampledColumns(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 160, 100))
	next := image.NewRGBA(first.Rect)
	for y := 0; y < 100; y++ {
		next.SetRGBA(1, y, color.RGBA{255, 255, 255, 255})
	}
	acc := New(Options{})
	if _, err := acc.Add(first); err != nil {
		t.Fatal(err)
	}
	if acc.SameAsPrevious(next) {
		t.Fatal("changed unsampled column was called identical")
	}
}

func TestStitchNarrowDetailDisambiguatesRepetition(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 96, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 96; x++ {
			v := uint8((y % 8) * 20)
			source.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
		// Every fourth column repeats; this one-pixel feature distinguishes
		// the true offset from the tempting larger eight-row aliases.
		source.SetRGBA(1, y, color.RGBA{uint8(y * 71), uint8(y * 119), uint8(y * 43), 255})
	}
	acc := New(Options{})
	if _, err := acc.Add(frame(source, 0, 200)); err != nil {
		t.Fatal(err)
	}
	if overlap, err := acc.Add(frame(source, 64, 200)); err != nil || overlap != 136 {
		t.Fatalf("repeating content overlap = %d, %v; want 136", overlap, err)
	}
	got := acc.Result().Image
	if got.Rect.Dy() != 264 || !bytes.Equal(got.Pix[:got.Stride*264], frame(source, 0, 264).Pix) {
		t.Fatal("narrow feature lost or duplicated rows")
	}
}

func TestStitchSkipsStickyHeaderOnEveryFrame(t *testing.T) {
	const header, viewport, scroll = 20, 120, 45
	content := tallImage(96, 240)
	stickyFrame := func(top int) *image.RGBA {
		out := image.NewRGBA(image.Rect(0, 0, 96, viewport))
		for y := 0; y < header; y++ {
			for x := 0; x < out.Rect.Dx(); x++ {
				out.SetRGBA(x, y, color.RGBA{12, 34, 56, 255})
			}
		}
		for y := header; y < viewport; y++ {
			copy(out.Pix[y*out.Stride:], content.Pix[(top+y-header)*content.Stride:])
		}
		return out
	}
	acc := New(Options{})
	if _, err := acc.Add(stickyFrame(0)); err != nil {
		t.Fatal(err)
	}
	overlap, err := acc.Add(stickyFrame(scroll))
	if err != nil || overlap != viewport-header-scroll {
		t.Fatalf("sticky overlap = %d, %v; want %d", overlap, err, viewport-header-scroll)
	}
	got := acc.Result().Image
	want := image.NewRGBA(image.Rect(0, 0, 96, header+viewport-header+scroll))
	copy(want.Pix, stickyFrame(0).Pix)
	for y := header; y < want.Rect.Dy(); y++ {
		copy(want.Pix[y*want.Stride:], content.Pix[(y-header)*content.Stride:])
	}
	if got.Rect != want.Rect || !bytes.Equal(got.Pix[:got.Stride*got.Rect.Dy()], want.Pix) {
		t.Fatal("sticky header was duplicated or content rows were misplaced")
	}
}

func TestStitchCapsRowsByPixelBudget(t *testing.T) {
	// A shallow frame tests the chosen row cap without allocating the cap.
	acc := New(Options{MaxHeight: int(^uint(0) >> 1)})
	if _, err := acc.Add(image.NewRGBA(image.Rect(0, 0, 8192, 2))); err != nil {
		t.Fatal(err)
	}
	if acc.opts.MaxHeight != 4096 {
		t.Fatalf("max rows = %d, want 4096 for an 8192-wide RGBA canvas", acc.opts.MaxHeight)
	}
	if acc.opts.MaxHeight > imagebudget.MaxPixels/acc.canvas.Rect.Dx() {
		t.Fatal("canvas can exceed pixel budget")
	}
}

func TestStitchRejectsOversizedOrMalformedFrameBeforeMutation(t *testing.T) {
	acc := New(Options{})
	for _, img := range []*image.RGBA{
		{Rect: image.Rect(0, 0, 8192, 4097)},
		{Rect: image.Rect(0, 0, 4, 4), Stride: 16, Pix: make([]byte, 2)},
		{Rect: image.Rect(0, 0, 4, 4), Stride: int(^uint(0) >> 1), Pix: make([]byte, 64)},
	} {
		if _, err := acc.Add(img); err == nil {
			t.Fatal("accepted oversized/malformed frame")
		}
		if height, frames := acc.Size(); height != 0 || frames != 0 {
			t.Fatal("rejected frame mutated accumulator")
		}
	}
}

func BenchmarkMatchSmallScroll(b *testing.B) {
	source := tallImage(640, 1203)
	prev, next := frame(source, 0, 1200), frame(source, 3, 1200)
	opts := (Options{}).withDefaults(1200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := findOverlap(prev, next, opts); got != 1197 {
			b.Fatalf("overlap %d", got)
		}
	}
}

func BenchmarkMatchNoisyRepetition(b *testing.B) {
	prev, next := image.NewRGBA(image.Rect(0, 0, 640, 1200)), image.NewRGBA(image.Rect(0, 0, 640, 1200))
	for i := range prev.Pix {
		prev.Pix[i] = 120
		next.Pix[i] = 124
	}
	opts := (Options{}).withDefaults(1200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		findOverlap(prev, next, opts)
	}
}
