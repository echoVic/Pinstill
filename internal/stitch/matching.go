package stitch

import (
	"image"
	"math"
)

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

const (
	signatureBins   = 8
	probeRows       = 12
	matchCandidates = 8
	maxRowRepeats   = 16
)

type rowSignature [signatureBins * 3]float64

type overlapMatch struct {
	overlap int
	nextEnd int
	footer  int
	diff    float64
}

type shiftCandidate struct {
	shift int
	score float64
}

type shiftCandidates struct {
	items [matchCandidates]shiftCandidate
	count int
}

func (candidates *shiftCandidates) add(shift int, score float64) {
	index := candidates.count
	for index > 0 && score < candidates.items[index-1].score {
		index--
	}
	if index >= len(candidates.items) {
		return
	}
	candidates.count = min(candidates.count+1, len(candidates.items))
	copy(candidates.items[index+1:candidates.count], candidates.items[index:candidates.count-1])
	candidates.items[index] = shiftCandidate{shift, score}
}

func findOverlap(prev, next *image.RGBA, opts Options) int {
	return matchOverlap(prev, next, opts).overlap
}

// Nominate shifts using matched rows first and bounded color probes as a
// fallback. Verify the shortlist against the full-width content, with a
// limited allowance for stationary or animated strips at the viewport edges.
func matchOverlap(prev, next *image.RGBA, opts Options) overlapMatch {
	height := prev.Rect.Dy()
	maxShift := height - opts.MinOverlap
	if maxShift < 1 || height != next.Rect.Dy() || opts.MaxOverlap < opts.MinOverlap {
		return overlapMatch{}
	}
	prevRows, nextRows := fingerprintRows(prev), fingerprintRows(next)
	fixedTop, fixedBottom := fixedEdges(prevRows, nextRows)
	rowDiffs := make([]float64, height)
	var best overlapMatch
	bestShift := 0
	runnerUp := math.Inf(1)
	visited := make([]bool, maxShift+1)
	consider := func(candidates shiftCandidates) {
		for _, candidate := range candidates.items[:candidates.count] {
			if visited[candidate.shift] {
				continue
			}
			visited[candidate.shift] = true
			overlap := height - candidate.shift
			for row := 0; row < overlap; row++ {
				rowDiffs[row] = 0
				if prevRows[row+candidate.shift] != nextRows[row] {
					rowDiffs[row] = meanAbsDiffAt(prev, next, row+candidate.shift, row, 1, 0)
				}
			}
			match := verifyOverlap(rowDiffs[:overlap], candidate.shift, fixedTop, fixedBottom, opts)
			if match.overlap == 0 {
				continue
			}
			// A small absolute tie margin rejects approximate aliases as
			// well as exact repetition. A clean run outranks noisy aliases.
			if bestShift == 0 || match.diff < best.diff {
				if bestShift != 0 {
					runnerUp = best.diff
				}
				best, bestShift = match, candidate.shift
			} else {
				runnerUp = min(runnerUp, match.diff)
			}
		}
	}
	consider(fingerprintCandidates(prevRows, nextRows, maxShift))
	if bestShift == 0 || best.diff > 0 {
		consider(colorCandidates(prev, next, maxShift))
	}
	const tieMargin = 0.04
	if bestShift == 0 || runnerUp <= best.diff+tieMargin {
		return overlapMatch{}
	}
	prevTop := best.nextEnd - best.overlap + bestShift
	if meanAbsDiffAt(prev, next, prevTop, best.nextEnd-best.overlap, best.overlap, opts.Tolerance) > opts.Tolerance {
		return overlapMatch{}
	}
	return best
}

func fingerprintCandidates(prev, next []uint64, maxShift int) shiftCandidates {
	type occurrences struct{ index, count int }
	locations := make(map[uint64]occurrences, len(next))
	previous := make([]int, len(next))
	for row, hash := range next {
		found := locations[hash]
		previous[row] = found.index
		locations[hash] = occurrences{row + 1, found.count + 1}
	}
	votes := make([]int, maxShift+1)
	for row, hash := range prev {
		found := locations[hash]
		if found.count > maxRowRepeats {
			continue
		}
		for index := found.index; index > 0; index = previous[index-1] {
			shift := row - (index - 1)
			if shift > 0 && shift <= maxShift {
				votes[shift]++
			}
		}
	}
	var result shiftCandidates
	for shift := 1; shift <= maxShift; shift++ {
		if votes[shift] >= 2 {
			result.add(shift, -float64(votes[shift]))
		}
	}
	return result
}

func fixedEdges(prev, next []uint64) (top, bottom int) {
	limit := len(prev) / 4
	for top < limit && prev[top] == next[top] {
		top++
	}
	for bottom < limit && prev[len(prev)-1-bottom] == next[len(next)-1-bottom] {
		bottom++
	}
	return top, bottom
}

func verifyOverlap(rows []float64, shift, fixedTop, fixedBottom int, opts Options) overlapMatch {
	if opts.MinOverlap > len(rows) {
		return overlapMatch{}
	}
	first, end := 0, len(rows)
	for row := 0; row < min(fixedTop, end); row++ {
		if rows[row] > opts.Tolerance {
			first = min(fixedTop, end)
			break
		}
	}
	for row := max(first, end-fixedBottom); row < end; row++ {
		if rows[row] > opts.Tolerance {
			end = max(first, end-fixedBottom)
			break
		}
	}
	// A three-row run anchors each edge. Preserve an isolated bad row as
	// noise when it would otherwise make a one-row hole in the content.
	for end-first >= 3 && !goodRun(rows[first:first+3], opts.Tolerance) {
		first++
	}
	for end-first >= 3 && !goodRun(rows[end-3:end], opts.Tolerance) {
		end--
	}
	if first == 1 {
		first = 0
	}
	if len(rows)-end == 1 {
		end = len(rows)
	}
	count := end - first
	footer := len(rows) - end
	maxEdge := (len(rows) + shift) / 4
	if count < opts.MinOverlap || count > opts.MaxOverlap || first > maxEdge || footer > maxEdge {
		return overlapMatch{}
	}
	var total float64
	good := 0
	for _, diff := range rows[first:end] {
		total += diff
		if diff <= opts.Tolerance {
			good++
		}
	}
	average := total / float64(count)
	if good*5 < count*4 || average > opts.Tolerance {
		return overlapMatch{}
	}
	return overlapMatch{overlap: count, nextEnd: end, footer: footer, diff: average}
}

func goodRun(rows []float64, tolerance float64) bool {
	for _, diff := range rows {
		if diff > tolerance {
			return false
		}
	}
	return true
}

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

func colorCandidates(prev, next *image.RGBA, maxShift int) shiftCandidates {
	previous, following := signatures(prev), signatures(next)
	var result shiftCandidates
	for shift := 1; shift <= maxShift; shift++ {
		overlap := len(previous) - shift
		var total, worst, secondWorst float64
		for probe := 0; probe < probeRows; probe++ {
			row := (2*probe + 1) * overlap / (2 * probeRows)
			var difference float64
			for channel, value := range previous[row+shift] {
				difference += math.Abs(value - following[row][channel])
			}
			total += difference
			if difference >= worst {
				worst, secondWorst = difference, worst
			} else {
				secondWorst = max(secondWorst, difference)
			}
		}
		result.add(shift, total-worst-secondWorst)
	}
	return result
}
