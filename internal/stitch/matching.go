package stitch

import "image"

func fingerprintRows(img *image.RGBA) []uint64 {
	out := make([]uint64, img.Rect.Dy())
	for y := range out {
		off := img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y)
		hash := uint64(14695981039346656037)
		for x := 0; x < img.Rect.Dx(); x++ {
			for c := 0; c < 3; c++ {
				hash = (hash ^ uint64(img.Pix[off+x*4+c])) * 1099511628211
			}
		}
		out[y] = hash
	}
	return out
}

// KMP finds the longest suffix of prev equal to a prefix of next. Failure
// links also enumerate smaller exact overlaps without rescanning the pixels.
// Fingerprints nominate candidates only; the caller verifies actual pixels.
func exactOverlap(prev, next []uint64, minOverlap, maxOverlap int) int {
	failure := make([]int, len(next))
	for i, j := 1, 0; i < len(next); i++ {
		for j > 0 && next[i] != next[j] {
			j = failure[j-1]
		}
		if next[i] == next[j] {
			j++
		}
		failure[i] = j
	}
	j := 0
	for _, row := range prev {
		for j > 0 && (j == len(next) || row != next[j]) {
			j = failure[j-1]
		}
		if row == next[j] {
			j++
		}
	}
	for j > maxOverlap {
		j = failure[j-1]
	}
	if j < minOverlap {
		return 0
	}
	return j
}

const (
	signatureBins   = 8
	probeRows       = 12
	matchCandidates = 8
)

type rowSignature [signatureBins * 3]float64

// Each column contributes to a spatial/color mean. Unlike fixed-stride
// sampling this does not erase a narrow feature at an unsampled column.
func signatures(img *image.RGBA) []rowSignature {
	out := make([]rowSignature, img.Rect.Dy())
	w := img.Rect.Dx()
	for y := range out {
		off := img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y)
		for bin := 0; bin < signatureBins; bin++ {
			start, end := bin*w/signatureBins, (bin+1)*w/signatureBins
			if start == end {
				continue
			}
			for x := start; x < end; x++ {
				for c := 0; c < 3; c++ {
					out[y][bin*3+c] += float64(img.Pix[off+x*4+c])
				}
			}
			for c := 0; c < 3; c++ {
				out[y][bin*3+c] /= float64(end - start)
			}
		}
	}
	return out
}

func noisyOverlap(prev, next *image.RGBA, opts Options, maxOverlap, nextTop int) int {
	p, n := signatures(prev), signatures(next)
	type candidate struct {
		overlap int
		score   float64
	}
	var best [matchCandidates]candidate
	count := 0
	for overlap := maxOverlap; overlap >= opts.MinOverlap; overlap-- {
		if nextTop+overlap > len(n) {
			continue
		}
		var score float64
		for i := 0; i < probeRows; i++ {
			y := i * (overlap - 1) / (probeRows - 1)
			for k, a := range p[len(p)-overlap+y] {
				diff := a - n[nextTop+y][k]
				if diff < 0 {
					diff = -diff
				}
				score += diff
			}
		}
		at := count
		for at > 0 && score < best[at-1].score {
			at--
		}
		if at >= len(best) {
			continue
		}
		if count < len(best) {
			count++
		}
		copy(best[at+1:count], best[at:count-1])
		best[at] = candidate{overlap, score}
	}
	overlap, bestDiff := 0, opts.Tolerance+1
	for _, c := range best[:count] {
		diff := meanAbsDiffAt(prev, next, len(p)-c.overlap, nextTop, c.overlap, 1, opts.Tolerance)
		if diff <= opts.Tolerance && (diff < bestDiff || diff == bestDiff && c.overlap > overlap) {
			overlap, bestDiff = c.overlap, diff
		}
	}
	return overlap
}
