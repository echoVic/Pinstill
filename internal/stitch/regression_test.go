package stitch

import (
	"image"
	"runtime"
	"testing"
)

func TestStitchSubimagesWithDifferentOrigins(t *testing.T) {
	source := tallImage(40, 180)
	first := source.SubImage(image.Rect(7, 10, 31, 110)).(*image.RGBA)
	second := source.SubImage(image.Rect(7, 70, 31, 170)).(*image.RGBA)
	acc := New(Options{})
	if _, err := acc.Add(first); err != nil {
		t.Fatal(err)
	}
	if !acc.SameAsPrevious(first) {
		t.Fatal("identical subimage was not recognized")
	}
	if overlap, err := acc.Add(second); err != nil || overlap != 40 {
		t.Fatalf("Add(second) = %d, %v; want overlap 40", overlap, err)
	}
	got := acc.Result().Image
	if got.Bounds() != image.Rect(0, 0, 24, 160) {
		t.Fatalf("result bounds = %v, want 24x160", got.Bounds())
	}
	for y := 0; y < 160; y++ {
		for x := 0; x < 24; x++ {
			if got.RGBAAt(x, y) != source.RGBAAt(x+7, y+10) {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got.RGBAAt(x, y), source.RGBAAt(x+7, y+10))
			}
		}
	}
}

func invertFirstRow(img *image.RGBA) {
	for x := 0; x < img.Rect.Dx(); x++ {
		for c := 0; c < 3; c++ {
			img.Pix[x*4+c] = 255 - img.Pix[x*4+c]
		}
	}
}

func TestStitchOverlapToleratesLocalNoise(t *testing.T) {
	source := tallImage(160, 300)
	acc := New(Options{})
	if _, err := acc.Add(frame(source, 0, 200)); err != nil {
		t.Fatal(err)
	}
	next := frame(source, 100, 200)
	// One changed row contributes at most 255/100 = 2.55 to the full
	// overlap's mean difference, within the default tolerance of 6.
	invertFirstRow(next)
	if overlap, err := acc.Add(next); err != nil || overlap != 100 {
		t.Fatalf("Add(noisy overlap) = %d, %v; want overlap 100", overlap, err)
	}
	if height, _ := acc.Size(); height != 300 {
		t.Fatalf("height = %d, want 300", height)
	}
}

func TestSameAsPreviousToleratesLocalNoise(t *testing.T) {
	source := tallImage(160, 500)
	acc := New(Options{})
	if _, err := acc.Add(source); err != nil {
		t.Fatal(err)
	}
	next := frame(source, 0, 500)
	// Even the maximum possible difference in one row gives a full-frame
	// mean of only 255/500 = 0.51, below the default threshold of 0.6.
	invertFirstRow(next)
	if !acc.SameAsPrevious(next) {
		t.Fatal("local noise below the full-frame threshold was treated as scrolling")
	}
}

func TestAddRejectsChangedDimensionsWithoutLosingPreviousFrame(t *testing.T) {
	for _, size := range []image.Point{{X: 32, Y: 200}, {X: 64, Y: 100}} {
		t.Run(size.String(), func(t *testing.T) {
			source := tallImage(64, 300)
			acc := New(Options{})
			if _, err := acc.Add(frame(source, 0, 200)); err != nil {
				t.Fatal(err)
			}
			if _, err := acc.Add(tallImage(size.X, size.Y)); err == nil {
				t.Error("dimension change silently accepted")
			}
			if height, count := acc.Size(); height != 200 || count != 1 {
				t.Errorf("Size() = %d, %d after rejected frame; want 200, 1", height, count)
			}
			if overlap, err := acc.Add(frame(source, 100, 200)); err != nil || overlap != 100 {
				t.Errorf("Add(valid frame) = %d, %v; want overlap 100", overlap, err)
			}
			if height, count := acc.Size(); height != 300 || count != 2 {
				t.Errorf("Size() = %d, %d after valid frame; want 300, 2", height, count)
			}
		})
	}
}

func TestResultReportsLastAppendedOverlap(t *testing.T) {
	source := tallImage(64, 400)
	acc := New(Options{MaxHeight: 250})
	for _, top := range []int{0, 100} {
		if _, err := acc.Add(frame(source, top, 200)); err != nil {
			t.Fatal(err)
		}
	}
	if got := acc.Result().Overlap; got != 100 {
		t.Fatalf("Result().Overlap = %d, want 100", got)
	}
	if _, err := acc.Add(frame(source, 200, 200)); err != nil {
		t.Fatal(err)
	}
	if got := acc.Result().Overlap; got != 100 {
		t.Fatalf("Result().Overlap after cap = %d, want last appended overlap 100", got)
	}
}

func TestStitchManyFramesHasBoundedAllocation(t *testing.T) {
	const width, height, step, frames = 320, 48, 16, 64
	const resultHeight = height + step*(frames-1)
	source := uniqueRowImage(width, resultHeight)
	// A fixed 64-frame run keeps this regression bounded under -race too.
	// No tests in this package run in parallel while allocation is sampled.
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	acc := New(Options{MaxHeight: resultHeight})
	for frame := 0; frame < frames; frame++ {
		top := frame * step
		if _, err := acc.Add(source.SubImage(image.Rect(0, top, width, top+height)).(*image.RGBA)); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	if got := acc.Result(); got.Image.Rect.Dy() != resultHeight || !got.ReachedMax {
		t.Fatal("canvas growth lost rows or missed the height cap")
	}
	// Copying the entire result on each append consumes over 30 times the
	// final image size here. Allow four times, including allocation overhead.
	const resultBytes = width * resultHeight * 4
	const budget = 4 * resultBytes
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > budget {
		t.Fatalf("allocated %d bytes for a %d-byte result; budget %d", allocated, resultBytes, budget)
	}
	t.Logf("allocated %d bytes for a %d-byte result", allocated, resultBytes)
}
