package stitch

import (
	"errors"
	"image"
	"image/color"
	"testing"
)

// tallImage builds a deterministic tall image whose rows are unique enough
// for overlap matching.
func tallImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((y*7 + x*3) % 256),
				G: uint8((y*11 + x*5) % 256),
				B: uint8((y*13 + x*17) % 256),
				A: 255,
			})
		}
	}
	return img
}

func frame(img *image.RGBA, top, height int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), height))
	for y := 0; y < height; y++ {
		copy(out.Pix[y*out.Stride:(y+1)*out.Stride], img.Pix[(top+y)*img.Stride:(top+y+1)*img.Stride])
	}
	return out
}

func meanDiff(a, b *image.RGBA) float64 {
	if a.Bounds() != b.Bounds() {
		return 255
	}
	bnd := a.Bounds()
	var total float64
	var count float64
	for y := 0; y < bnd.Dy(); y++ {
		for x := 0; x < bnd.Dx(); x += 3 {
			ai := a.PixOffset(x, y)
			bi := b.PixOffset(x, y)
			total += float64(absInt(int(a.Pix[ai])-int(b.Pix[bi])) + absInt(int(a.Pix[ai+1])-int(b.Pix[bi+1])) + absInt(int(a.Pix[ai+2])-int(b.Pix[bi+2])))
			count += 3
		}
	}
	return total / count
}

func TestStitchReconstructsScrolledFrames(t *testing.T) {
	tall := tallImage(160, 1200)
	acc := New(Options{})
	frames := []*image.RGBA{frame(tall, 0, 400), frame(tall, 250, 400), frame(tall, 500, 400), frame(tall, 750, 400)}
	for _, f := range frames {
		if _, err := acc.Add(f); err != nil {
			t.Fatal(err)
		}
	}
	height, count := acc.Size()
	if count != len(frames) {
		t.Fatalf("frames = %d, want %d", count, len(frames))
	}
	wantHeight := 750 + 400
	if height != wantHeight {
		t.Fatalf("height = %d, want %d", height, wantHeight)
	}
	res := acc.Result()
	want := frame(tall, 0, wantHeight)
	if diff := meanDiff(res.Image, want); diff > 1 {
		t.Fatalf("stitched image differs from source: mean diff %.2f", diff)
	}
}

func TestStitchRejectsUnrelatedFrame(t *testing.T) {
	acc := New(Options{})
	first := tallImage(80, 200)
	second := tallImage(80, 200)
	// Make the second frame unrelated to the first.
	for i := range second.Pix {
		second.Pix[i] = 255 - second.Pix[i]
	}
	if _, err := acc.Add(first); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.Add(second); !errors.Is(err, ErrUnaligned) {
		t.Fatalf("Add(unrelated frame) error = %v", err)
	}
	height, _ := acc.Size()
	if height != 200 {
		t.Fatalf("height = %d, want 200", height)
	}
}

func TestStitchDetectsStoppedScroll(t *testing.T) {
	tall := tallImage(64, 400)
	acc := New(Options{})
	if _, err := acc.Add(frame(tall, 0, 200)); err != nil {
		t.Fatal(err)
	}
	if !acc.SameAsPrevious(frame(tall, 0, 200)) {
		t.Fatal("identical frame must count as stopped")
	}
	scrolled := frame(tall, 120, 200)
	acc2 := New(Options{})
	if _, err := acc2.Add(frame(tall, 0, 200)); err != nil {
		t.Fatal(err)
	}
	if acc2.SameAsPrevious(scrolled) {
		t.Fatal("scrolled frame must not count as stopped")
	}
}

func TestStitchStopsAtMaxHeight(t *testing.T) {
	tall := tallImage(64, 2000)
	acc := New(Options{MaxHeight: 500})
	if _, err := acc.Add(frame(tall, 0, 300)); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 5; i++ {
		if _, err := acc.Add(frame(tall, i*150, 300)); err != nil {
			t.Fatal(err)
		}
	}
	height, _ := acc.Size()
	if height > 500 {
		t.Fatalf("height = %d, max 500", height)
	}
	if !acc.Result().ReachedMax {
		t.Fatal("ReachedMax = false")
	}
}

func TestAddRejectsEmptyFrame(t *testing.T) {
	acc := New(Options{})
	if _, err := acc.Add(nil); err == nil {
		t.Fatal("nil frame accepted")
	}
	if _, err := acc.Add(image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Fatal("empty frame accepted")
	}
}

// TestFirstFrameTallerThanMaxHeightIsCapped covers the height cap applying to
// the very first frame: Size()/Result() must agree on the capped height instead
// of reporting more rows than the image contains.
func TestFirstFrameTallerThanMaxHeightIsCapped(t *testing.T) {
	acc := New(Options{MaxHeight: 100})
	if _, err := acc.Add(tallImage(32, 300)); err != nil {
		t.Fatalf("Add(first frame) error = %v", err)
	}
	height, frames := acc.Size()
	if frames != 1 {
		t.Fatalf("frames = %d, want 1", frames)
	}
	if height != 100 {
		t.Fatalf("height = %d, want the capped 100", height)
	}
	res := acc.Result()
	if res.Image == nil {
		t.Fatal("Result().Image = nil")
	}
	if bounds := res.Image.Bounds(); bounds.Dx() != 32 || bounds.Dy() != 100 {
		t.Fatalf("result image = %v, want 32x100", bounds)
	}
	if !res.ReachedMax {
		t.Fatal("ReachedMax = false for a capped first frame")
	}
	if res.Frames != 1 {
		t.Fatalf("Result().Frames = %d, want 1", res.Frames)
	}

	// Once capped, later frames cannot grow the canvas either.
	if _, err := acc.Add(tallImage(32, 300)); err != nil {
		t.Fatalf("Add(later frame) error = %v", err)
	}
	if height, _ := acc.Size(); height != 100 {
		t.Fatalf("height after another frame = %d, want 100", height)
	}
	if bounds := acc.Result().Image.Bounds(); bounds.Dy() != 100 {
		t.Fatalf("result image height after another frame = %d, want 100", bounds.Dy())
	}
}
