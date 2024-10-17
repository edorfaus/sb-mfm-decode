package filter

import (
	"fmt"

	"github.com/edorfaus/sb-mfm-decode/log"
)

type DCOffset struct {
	NoiseFloor int
	PeakWidth  int

	data   []int
	offset int
}

func NewDCOffset(noiseFloor, peakWidth int) *DCOffset {
	return &DCOffset{
		NoiseFloor: noiseFloor,
		PeakWidth:  peakWidth,
	}
}

func bc(v bool, ft string) byte {
	if v {
		return ft[1]
	}
	return ft[0]
}

func (f *DCOffset) Run(data []int) []int {
	out := make([]int, len(data))

	nf := f.NoiseFloor
	pw := f.PeakWidth
	if pw <= 0 {
		pw = 48000 / 4800
		f.PeakWidth = pw
	}

	f.data = data
	f.offset = 0
	i := 0
	setOffset := func(v int) {
		f.offset = v
		out[i] = v
		log.F(4, " | ofs=%v\n", v)
	}
	sz := len(fmt.Sprint(len(data)))
	log.Ln(4)
	for ; i < len(data); i++ {
		log.F(4, "%*v:", sz, i)
		// Get the ends of the peak we are currently in, if any
		back, forward := i, i

		v := data[i] - f.offset
		log.F(4, " %6v -%6v =%6v", data[i], f.offset, v)
		if abs(v) > nf {
			back = f.findNoiseOrEdge(i, -1)
			forward = f.findNoiseOrEdge(i, 1)
		}
		log.F(4, " B:%*v F:%*v", sz, back, sz, forward)

		// Find the next peak in either direction
		back2 := f.findPeak(back, -1)
		forward2 := f.findPeak(forward, 1)

		log.F(4, " b:%*v f:%*v", sz, back2, sz, forward2)

		// Get low/high values for these peaks
		bLow, bHigh := lowHigh(data[back2 : back+1])
		hLow, hHigh := lowHigh(data[back : forward+1])
		fLow, fHigh := lowHigh(data[forward : forward2+1])

		log.F(
			4, " L:%6v,%6v,%6v H:%6v,%6v,%6v", bLow, hLow, fLow,
			bHigh, hHigh, fHigh,
		)

		// Check which peaks we have, and whether to look for more
		allLow := min(bLow, min(hLow, fLow))
		allHigh := max(bHigh, max(hHigh, fHigh))

		hasLow := allLow-f.offset < -nf
		hasHigh := allHigh-f.offset > nf

		hasBack := abs(bLow-f.offset) > nf || abs(bHigh-f.offset) > nf
		hasFwd := abs(fLow-f.offset) > nf || abs(fHigh-f.offset) > nf

		log.F(
			4, " has:%c%c%c%c", bc(hasLow, "lL"), bc(hasHigh, "hH"),
			bc(hasBack, "bB"), bc(hasFwd, "fF"),
		)

		if hasLow && hasHigh {
			setOffset((allLow + allHigh) / 2)
			continue
		}

		// We are missing a peak type, having only high or low, or none.

		if !hasBack && !hasFwd {
			// No peaks in either direction; are we at a peak?
			if hasLow || hasHigh {
				// We are at a peak, but with no inverse peak to look at
				// it would skew the results, so ignore the peak.
				allLow = min(bLow, fLow)
				allHigh = max(bHigh, fHigh)
			}
			// If we are not at a peak, then this area is only noise.
			setOffset((allLow + allHigh) / 2)
			continue
		}

		// This area has one or more peaks, but all of them go in the
		// same direction, so we cannot use any of them.
		// Fall back to using only the area that is nothing but noise,
		// just like we do when there's only noise in the entire area.
		switch {
		case !hasBack:
			setOffset((bLow + bHigh) / 2)
		case !hasFwd:
			setOffset((fLow + fHigh) / 2)
		case allHigh-allLow < nf:
			// This area contains no peaks (it is as flat as noise), but
			// the current offset pushes it outside of the noise floor.
			// Reset the offset so that this is at least not permanent.
			setOffset((allLow + allHigh) / 2)
		default:
			// Peaks in both directions, but they go the same direction;
			// this shouldn't happen (given reasonable peak widths), but
			// we handle it anyway, simply by not changing the offset.
			setOffset(f.offset)
		}
	}
	log.Ln(4)

	return out
}

func (f *DCOffset) findPeak(at, dir int) int {
	pw, nf, data, offset := f.PeakWidth, f.NoiseFloor, f.data, f.offset

	// Find the first non-noise sample
	i, j := pw, at
	for i > 0 && j >= 0 && j < len(data) {
		if abs(data[j]-offset) > nf {
			// Found non-noise, now find the end of this peak
			return f.findNoiseOrEdge(j, dir)
		}
		i--
		j += dir
	}

	return j - dir
}

func (f *DCOffset) findNoiseOrEdge(at, dir int) int {
	data, offset, nf := f.data, f.offset, f.NoiseFloor
	if at < 0 || at >= len(data) || abs(data[at]-offset) <= nf {
		return at
	}
	if data[at]-offset < 0 {
		return f.findNoiseOrHigh(at, dir)
	}
	return f.findNoiseOrLow(at, dir)
}

func (f *DCOffset) findNoiseOrLow(at, dir int) int {
	pw, nf, data, offset := f.PeakWidth, f.NoiseFloor, f.data, f.offset
	for i := pw * 4; i > 0 && at >= 0 && at < len(data); i-- {
		if data[at]-offset <= nf {
			return at
		}
		at += dir
	}
	return at - dir
}

func (f *DCOffset) findNoiseOrHigh(at, dir int) int {
	pw, nf, data, offset := f.PeakWidth, f.NoiseFloor, f.data, f.offset
	for i := pw * 4; i > 0 && at >= 0 && at < len(data); i-- {
		if data[at]-offset >= -nf {
			return at
		}
		at += dir
	}
	return at - dir
}
