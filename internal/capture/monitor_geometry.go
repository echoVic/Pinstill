package capture

import (
	"errors"
	"image"
	"math"
	"sort"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

type monitorGeometry struct {
	pixels image.Rectangle
	dpi    int
}

func monitorPixelRect(rect Rect, monitors []monitorGeometry) ([4]int, error) {
	if rect.Empty() || len(monitors) == 0 {
		return [4]int{}, errors.New("capture: invalid rectangle or missing monitors")
	}
	// MyGo v0.9.0 internal/windows/dips.go: nearest integer, halves UP,
	// including negative values (unlike math.Round's halves away from zero).
	r := [4]int64{}
	for i, v := range []float64{rect.X, rect.Y, rect.Width, rect.Height} {
		v = math.Floor(v + 0.5)
		if v < math.MinInt32 || v > math.MaxInt32 {
			return [4]int{}, errors.New("capture: rectangle exceeds Win32 coordinates")
		}
		r[i] = int64(v)
	}
	r[2], r[3] = max(1, r[2]), max(1, r[3])
	minDPI := int64(math.MaxInt32)
	for _, m := range monitors {
		if m.dpi <= 0 || m.dpi > math.MaxInt32 || m.pixels.Empty() {
			return [4]int{}, errors.New("capture: invalid monitor geometry")
		}
		for _, v := range []int{m.pixels.Min.X, m.pixels.Min.Y, m.pixels.Max.X, m.pixels.Max.Y} {
			if int64(v) < math.MinInt32 || int64(v) > math.MaxInt32 {
				return [4]int{}, errors.New("capture: invalid monitor bounds")
			}
		}
		minDPI = min(minDPI, int64(m.dpi))
	}
	physical := make([][4]int64, len(monitors))
	logical := make([][4]int64, len(monitors))
	for i, m := range monitors {
		p := [4]int64{int64(m.pixels.Min.X), int64(m.pixels.Min.Y), int64(m.pixels.Max.X) - int64(m.pixels.Min.X), int64(m.pixels.Max.Y) - int64(m.pixels.Min.Y)}
		physical[i] = p
		logical[i] = [4]int64{scaleDIP(p[0], 96, minDPI), scaleDIP(p[1], 96, minDPI), scaleDIP(p[2], 96, int64(m.dpi)), scaleDIP(p[3], 96, int64(m.dpi))}
	}
	toPixels := func(i int) [4]int64 {
		p, d, dpi := physical[i], logical[i], int64(monitors[i].dpi)
		return [4]int64{p[0] + scaleDIP(r[0]-d[0], dpi, 96), p[1] + scaleDIP(r[1]-d[1], dpi, 96), max(1, scaleDIP(r[2], dpi, 96)), max(1, scaleDIP(r[3], dpi, 96))}
	}
	// Reproduce rectToPixels' round-trip preference, including rectangles
	// spanning two displays. Selection/fullscreen/scroll use this same path.
	order := monitorOrder(r, logical)
	pixels := toPixels(order[0])
	for _, i := range order {
		candidate := toPixels(i)
		if monitorOrder(candidate, physical)[0] == i {
			pixels = candidate
			break
		}
	}
	if err := checkPixelRect(pixels); err != nil {
		return [4]int{}, err
	}
	return [4]int{int(pixels[0]), int(pixels[1]), int(pixels[2]), int(pixels[3])}, nil
}

// scaleDIP is MyGo's scaled() with 64-bit intermediates and quotient-first
// multiplication, avoiding overflow even for rejected extreme coordinates.
func scaleDIP(v, num, den int64) int64 {
	a := 2*(v%den)*num + den
	q := a / (2 * den)
	if a < 0 && a%(2*den) != 0 {
		q--
	}
	return (v/den)*num + q
}

func monitorOrder(r [4]int64, bounds [][4]int64) []int {
	type rank struct {
		i              int
		area, distance float64
	}
	ranks := make([]rank, len(bounds))
	for i, b := range bounds {
		w := min(r[0]+r[2], b[0]+b[2]) - max(r[0], b[0])
		h := min(r[1]+r[3], b[1]+b[3]) - max(r[1], b[1])
		k := rank{i: i}
		if w > 0 && h > 0 {
			k.area = float64(w) * float64(h)
		} else {
			dx, dy := float64(max(0, -w)), float64(max(0, -h))
			k.distance = dx*dx + dy*dy
		}
		ranks[i] = k
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		a, b := ranks[i], ranks[j]
		return a.area > b.area || a.area == b.area && a.distance < b.distance
	})
	order := make([]int, len(bounds))
	for i, k := range ranks {
		order[i] = k.i
	}
	return order
}

func checkPixelRect(r [4]int64) error {
	for _, v := range r {
		if v < math.MinInt32 || v > math.MaxInt32 {
			return errors.New("capture: pixel rectangle exceeds Win32 coordinates")
		}
	}
	if r[0]+r[2] > math.MaxInt32 || r[1]+r[3] > math.MaxInt32 {
		return errors.New("capture: pixel rectangle edge exceeds Win32 coordinates")
	}
	return imagebudget.CheckDimensions(int(r[2]), int(r[3]))
}

func clipPixelRect(rect, desktop [4]int) ([4]int, error) {
	r := [4]int64{int64(rect[0]), int64(rect[1]), int64(rect[2]), int64(rect[3])}
	if err := checkPixelRect(r); err != nil {
		return [4]int{}, err
	}
	d := [4]int64{int64(desktop[0]), int64(desktop[1]), int64(desktop[2]), int64(desktop[3])}
	if d[2] <= 0 || d[3] <= 0 {
		return [4]int{}, errors.New("capture: empty desktop")
	}
	left, top := max(r[0], d[0]), max(r[1], d[1])
	right, bottom := min(r[0]+r[2], d[0]+d[2]), min(r[1]+r[3], d[1]+d[3])
	clipped := [4]int64{left, top, right - left, bottom - top}
	if err := checkPixelRect(clipped); err != nil {
		return [4]int{}, err
	}
	return [4]int{int(left), int(top), int(right - left), int(bottom - top)}, nil
}
