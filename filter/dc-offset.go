package filter

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
	}
	for ; i < len(data); i++ {
		// Get the ends of the peak we are currently in, if any
		back, forward := i, i

		v := data[i] - f.offset
		if abs(v) > nf {
			back = f.findNoiseOrEdge(i, -1)
			forward = f.findNoiseOrEdge(i, 1)
		}

		// Find the next peak in either direction
		back2 := f.findPeak(back, -1)
		forward2 := f.findPeak(forward, 1)

		// Get low/high values for these peaks
		bLow, bHigh := lowHigh(data[back2 : back+1])
		hLow, hHigh := lowHigh(data[back : forward+1])
		fLow, fHigh := lowHigh(data[forward : forward2+1])

		// Check which peaks we have, and whether to look for more
		allLow := min(bLow, min(hLow, fLow))
		allHigh := max(bHigh, max(hHigh, fHigh))

		hasLow := allLow-f.offset < -nf
		hasHigh := allHigh-f.offset > nf

		if hasLow && hasHigh {
			setOffset((allLow + allHigh) / 2)
			continue
		}

		// We are missing a peak type, see if we can find it.
		hasBack := abs(bLow-f.offset) > nf || abs(bHigh-f.offset) > nf
		hasFwd := abs(fLow-f.offset) > nf || abs(fHigh-f.offset) > nf

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

		// We have at least one peak in this area, look for another.
		if hasBack {
			back3 := f.findPeak(back2, -1)
			b3Low, b3High := lowHigh(data[back3 : back2+1])
			if !hasLow && b3Low-f.offset < -nf {
				// This is a low peak, which was missing
				allLow = min(allLow, b3Low)
				hasLow = true
			}
			if !hasHigh && b3High-f.offset > nf {
				// This is a high peak, which was missing
				allHigh = max(allHigh, b3High)
				hasHigh = true
			}
		}
		if hasFwd && !(hasLow && hasHigh) {
			forward3 := f.findPeak(forward2, 1)
			f3Low, f3High := lowHigh(data[forward2 : forward3+1])
			if !hasLow && f3Low-f.offset < -nf {
				// This is a low peak, which was missing
				allLow = min(allLow, f3Low)
				hasLow = true
			}
			if !hasHigh && f3High-f.offset > nf {
				// This is a high peak, which was missing
				allHigh = max(allHigh, f3High)
				hasHigh = true
			}
		}

		if hasLow && hasHigh {
			setOffset((allLow + allHigh) / 2)
			continue
		}

		// We did not find another peak, so we cannot use any of them.
		// Fall back to using only the area that is nothing but noise,
		// just like we do when there's only noise in the entire area.
		switch {
		case !hasBack:
			setOffset((bLow + bHigh) / 2)
		case !hasFwd:
			setOffset((fLow + fHigh) / 2)
		default:
			// Peaks in both directions, but they go the same direction;
			// this shouldn't happen (given reasonable peak widths), but
			// we handle it anyway, simply by not changing the offset.
			setOffset(f.offset)
		}
	}

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
