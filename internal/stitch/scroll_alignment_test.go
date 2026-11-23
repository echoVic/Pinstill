package stitch

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestStitchDynamicHeaderAndFooter(t *testing.T) {
	const width, viewport, step = 96, 160, 38
	content := tallImage(width, 330)
	framed := func(offset, header, footer int, fill color.RGBA) *image.RGBA {
		shot := image.NewRGBA(image.Rect(0, 0, width, viewport))
		for row := 0; row < viewport; row++ {
			if row >= header && row < viewport-footer {
				copyRowsFrom(shot, row, content, offset+row, 1)
				continue
			}
			for column := 0; column < width; column++ {
				shot.SetRGBA(column, row, fill)
			}
		}
		return shot
	}
	first := framed(0, 23, 16, color.RGBA{25, 15, 95, 255})
	second := framed(step, 17, 21, color.RGBA{210, 80, 9, 255})
	third := framed(2*step, 26, 14, color.RGBA{44, 71, 190, 255})
	acc := New(Options{})
	for _, shot := range []*image.RGBA{first, second, third} {
		if _, err := acc.Add(shot); err != nil {
			t.Fatal(err)
		}
	}
	want := frame(content, 0, viewport+2*step)
	copyRows(want, 0, first, 23)
	copyRowsFrom(want, want.Rect.Dy()-14, third, viewport-14, 14)
	got := acc.Result().Image
	if got.Rect != want.Rect || !bytes.Equal(got.Pix[:len(want.Pix)], want.Pix) {
		t.Fatalf("expected one first header, uninterrupted body rows, and one final footer at %v, got %v", want.Rect, got.Rect)
	}
}

func TestStitchKeepsLastConnectedFrameAcrossGaps(t *testing.T) {
	const width, viewport = 96, 150
	page := tallImage(width, 240)
	acc := New(Options{})
	if _, err := acc.Add(frame(page, 0, viewport)); err != nil {
		t.Fatal(err)
	}
	unrelated := image.NewRGBA(image.Rect(0, 0, width, viewport))
	for row := 0; row < viewport; row++ {
		for column := 0; column < width; column++ {
			unrelated.SetRGBA(column, row, color.RGBA{14, 14, 14, 255})
		}
	}
	if _, err := acc.Add(unrelated); !errors.Is(err, ErrUnaligned) {
		t.Fatal("accepted a disconnected frame")
	}
	if height, count := acc.Size(); height != viewport || count != 1 {
		t.Fatalf("disconnected frame changed the accepted result: height=%d frames=%d", height, count)
	}
	if overlap, err := acc.Add(frame(page, 68, viewport)); err != nil || overlap != 82 {
		t.Fatalf("returning to connected content got overlap=%d, error=%v", overlap, err)
	}
}

func TestStitchRejectsAmbiguousRepeatingRows(t *testing.T) {
	const width, viewport = 100, 160
	page := image.NewRGBA(image.Rect(0, 0, width, 260))
	for row := 0; row < page.Rect.Dy(); row++ {
		for column := 0; column < width; column++ {
			value := uint8((row%12)*16 + column%5)
			page.SetRGBA(column, row, color.RGBA{value, value, value, 255})
		}
	}
	acc := New(Options{})
	if _, err := acc.Add(frame(page, 0, viewport)); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.Add(frame(page, 37, viewport)); !errors.Is(err, ErrUnaligned) {
		t.Fatal("ambiguous texture selected one of multiple exact offsets")
	}
	if height, count := acc.Size(); height != viewport || count != 1 {
		t.Fatalf("ambiguous texture changed result: height=%d frames=%d", height, count)
	}
}

func uniqueRowImage(width, height int) *image.RGBA {
	result := tallImage(width, height)
	for row := 0; row < height; row++ {
		result.SetRGBA(0, row, color.RGBA{uint8(row), uint8(row >> 8), uint8(row >> 16), 255})
	}
	return result
}

func TestSameAsPreviousIgnoresAnimatedEdgeChrome(t *testing.T) {
	const width, viewport = 100, 180
	page := tallImage(width, viewport+10)
	first := frame(page, 0, viewport)
	second := frame(page, 0, viewport)
	for row := 0; row < viewport; row++ {
		if row >= 22 && row < viewport-16 {
			continue
		}
		for column := 0; column < width; column++ {
			second.SetRGBA(column, row, color.RGBA{210, 20, 55, 255})
		}
	}
	acc := New(Options{})
	if _, err := acc.Add(first); err != nil {
		t.Fatal(err)
	}
	if !acc.SameAsPrevious(second) {
		t.Fatal("changes limited to top and bottom chrome were treated as motion")
	}
	if acc.SameAsPrevious(frame(page, 10, viewport)) {
		t.Fatal("the body visibly shifted")
	}
}

func TestSameAsPreviousDetectsSparseMovingDetails(t *testing.T) {
	const width, viewport, shift = 2000, 160, 37
	page := image.NewRGBA(image.Rect(0, 0, width, viewport+shift))
	for row := 0; row < page.Rect.Dy(); row++ {
		column := (row*73 + 133) % width
		page.SetRGBA(column, row, color.RGBA{255, 255, 255, 255})
	}
	first := frame(page, 0, viewport)
	second := frame(page, shift, viewport)
	acc := New(Options{})
	if _, err := acc.Add(first); err != nil {
		t.Fatal(err)
	}
	if acc.SameAsPrevious(second) {
		t.Fatal("sparse pixels moving throughout the viewport were treated as stationary")
	}
	if overlap, err := acc.Add(second); err != nil || overlap != viewport-shift {
		t.Fatalf("localized motion overlap = %d, %v; want %d", overlap, err, viewport-shift)
	}
}
