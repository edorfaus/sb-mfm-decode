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
	out    []int
	pos    int

	// noiseLevel is the level at which samples go from noise to data.
	// It is set to either NoiseFloor or a value calculated from nearby
	// peaks, whichever is higher at that point.
	noiseLevel int
}

func NewDCOffset(noiseFloor, peakWidth int) *DCOffset {
	return &DCOffset{
		NoiseFloor: noiseFloor,
		PeakWidth:  peakWidth,
		noiseLevel: noiseFloor,
	}
}

func (f *DCOffset) Run(input, output []int) error {
	if f.PeakWidth <= 0 {
		f.PeakWidth = 48000 / 4800
	}
	f.noiseLevel = f.NoiseFloor

	if len(output) < len(input) {
		return fmt.Errorf("output cannot be shorter than input")
	}

	defer func() {
		f.data, f.out = nil, nil
	}()

	f.data = input
	f.offset = 0
	f.out = output
	f.pos = 0
	for f.pos < len(f.data) {
		// Initial state: we're at the start of the leading noise
		f.leadingNoise()
		if f.pos >= len(f.data) {
			break
		}

		// We found the first peak after the noise, handle that peak
		// (along with the remaining noise leading up to it).
		if err := f.firstPeak(); err != nil {
			return fmt.Errorf("firstPeak: %w", err)
		}
		if !f.outsideNoise(f.pos) {
			// No next peak, so this was a single peak, and we're in the
			// noise again, needing to look for another first peak.
			// (Or we hit the end of the data, which the loop condition
			// will take care of checking for us.)
			continue
		}

		// We handled the first peak in a sequence of peaks; now handle
		// the next peak in that sequence (including the last peak).

		for f.outsideNoise(f.pos) {
			if err := f.nextPeak(); err != nil {
				return fmt.Errorf("nextPeak: %w", err)
			}
		}
	}

	return nil
}

func (f *DCOffset) outsideNoise(pos int) bool {
	data := f.data
	return pos < len(data) && abs(data[pos]-f.offset) > f.noiseLevel
}

func (f *DCOffset) withinNoise(pos int) bool {
	data := f.data
	return pos < len(data) && abs(data[pos]-f.offset) <= f.noiseLevel
}

// Move past the leading noise in the data, while adjusting the offset.
func (f *DCOffset) leadingNoise() {
	pw, nf, nl, data := f.PeakWidth, f.NoiseFloor, f.noiseLevel, f.data
	out, pos, offset := f.out, f.pos, f.offset

	for pos < len(data) {
		to := min(pos+pw, len(data))
		lo, hi := lowHigh(data[pos:to])
		dlo, dhi := abs(lo-offset), abs(hi-offset)
		if dlo > nl || dhi > nl {
			// Found a peak.
			break
		}

		if nl > nf {
			// Fade the noise level back towards the noise floor.
			// However, make sure that this doesn't cause a closer peak
			// by only doing it when we have some headroom.
			maxV := max(dlo, dhi)
			if maxV*2 < nl || maxV*4 < nf*3 {
				// We have some good headroom, so move it closer.
				nl = max(nf, nl-(nl/8))
			}
		}

		// No peak here, just noise, so adjust the offset by averaging
		// the old value with the new middle-point.
		offset = (offset + ((lo + hi) / 2)) / 2
		out[pos] = data[pos] - offset
		pos++
	}

	f.offset = offset
	f.pos = pos
	f.noiseLevel = nl
}

// Handle the first peak after the leading noise.
// If this is a lone peak, the position will be left in the noise after,
// or at the end of the data if the peak goes that far.
// Otherwise, the position will be left at the tip of the peak.
func (f *DCOffset) firstPeak() error {
	// This is only called with at most one peak-width of noise before
	// the peak starts. This peak is likely to mark a boundary where the
	// DC offset significantly changes, so look for the peak before
	// trying to handle the remaining leading noise.

	pw, data := f.PeakWidth, f.data

	start := f.pos
	for f.withinNoise(start) {
		start++
	}

	peak := f.findPeakAt(start)
	log.F(3, "First peak: %+v\n", peak)

	if peak.End < 0 {
		//log.Warn("peak too long at", start)
		// TODO: handle this, e.g. by re-doing with new offset based on
		// the min/max of the following area (longer than peak width).
		return fmt.Errorf("peak too long at %v", start)
	}
	if peak.Next >= len(data) {
		// This is a single peak that runs to the end of the data.
		// There's not much we can do here, so just apply the offset.
		log.Warn("single peak to end detected at", start)
		f.applyOffsetUntil(len(data))
		return nil
	}
	if f.withinNoise(peak.Next) {
		// This is a single peak that is followed by noise.
		// We don't want this lone peak to skew the offset too much, so
		// we instead find the offset of the noise after the peak, and
		// apply the average of that and the current offset.
		log.Warn("single peak detected at", start)
		// TODO: should we adjust the noiseLevel here? it might affect
		// whether there's a next peak detected, so we might have to
		// re-do the peak?
		to := min(peak.Next+pw, len(data))
		lo, hi := lowHigh(data[peak.Next:to])
		nextOffset := (lo + hi) / 2
		peakOffset := (f.offset + nextOffset) / 2
		f.handleLeadingEdge(peak, peakOffset)
		f.handleTrailingEdge(peak, nextOffset)
		return nil
	}

	// We found the first peak, and the start of the second.
	// Find the rest of the second peak, to find the overall DC offset.

	nextOffset := f.offset

	nextPeak := f.findPeakAt(peak.Next)
	log.F(3, "Second peak: %+v\n", nextPeak)

	if nextPeak.End < 0 {
		//log.Warn("next peak too long at", nextPeak.Start)
		// TODO: handle this somehow?
		return fmt.Errorf("next peak too long at %v", nextPeak.Start)
	}
	if nextPeak.Next >= len(data) {
		// This peak went off the end of the data, so we might not have
		// found its tip. Without that, the new offset would be wrong.
		// There's not much we can do here, so just keep the old offset.
		log.Warn("peak runs off end of data at", start)
	} else {
		nextOffset = (peak.Value + nextPeak.Value) / 2

		f.updateNoiseLevel(nextOffset, peak.Value, nextPeak.Value)
	}

	f.handleLeadingEdge(peak, nextOffset)

	return nil
}

// This applies the offset to the leading edge of the given peak, while
// ensuring that doing so does not create an artificial inverse peak.
// This is only intended to be used for the first peak in a group.
func (f *DCOffset) handleLeadingEdge(peak Peak, peakOffset int) {
	data, out := f.data, f.out

	// This works backwards, to properly detect the first zero crossing.
	// Apply the offset until the start, or until the data crosses zero.
	peakSign := data[peak.Index] < 0
	pos := peak.Index - 1
	for pos >= peak.Start {
		v := data[pos] - peakOffset
		if (v < 0) != peakSign {
			break
		}
		out[pos] = v
		pos--
	}

	// After crossing zero, we ensure the rest stays as within noise.
	// The first sample we want to be extra careful with, to keep the
	// crossing point as close to correct as possible. The rest we try
	// to move closer to the earlier offset, but still within noise.
	offset := peakOffset
	for pos >= f.pos {
		offset = f.clampToNoise(offset, data[pos])
		out[pos] = data[pos] - offset
		pos--
		// Move the offset closer to the earlier offset.
		offset = (offset + f.offset) / 2
	}

	f.offset = peakOffset
	f.pos = peak.Index
}

// This applies the offset to the trailing edge of the given peak, while
// ensuring that doing so does not create an artificial inverse peak.
// This is only intended to be used for the last peak in a group, and
// expects that the current position is at the tip of that peak.
func (f *DCOffset) handleTrailingEdge(peak Peak, nextOffset int) {
	data, out, offset, pos := f.data, f.out, f.offset, f.pos

	// Apply the offset until the end, or until the data crosses zero.
	peakSign := data[peak.Index] < 0
	for pos <= peak.End {
		v := data[pos] - offset
		if (v < 0) != peakSign {
			break
		}
		out[pos] = v
		pos++
	}

	// After crossing zero, we ensure the rest stays as within noise.
	// The first sample we want to be extra careful with, to keep the
	// crossing point as close to correct as possible. The rest we try
	// to move closer to the target offset, but still within noise.
	for pos < peak.Next {
		offset = f.clampToNoise(offset, data[pos])
		out[pos] = data[pos] - offset
		pos++
		// Move the offset closer to the next offset.
		offset = (offset + nextOffset) / 2
	}

	f.offset = nextOffset
	f.pos = pos
}

// clampToNoise clamps the given offset such that the given sample would
// be within the noise. If it already is, the offset is returned as-is.
func (f *DCOffset) clampToNoise(offset, val int) int {
	// Note: this purposely uses NoiseFloor instead of noiseLevel.
	nf := f.NoiseFloor
	if val-offset > nf {
		// we want v-ofs = nf => v = nf+ofs => v-nf = ofs
		return val - nf
	}
	if val-offset < -nf {
		// we want v-ofs = -nf => v = ofs-nf => ofs = v+nf
		return val + nf
	}
	return offset
}

// Handle the first peak after the leading noise.
// This expects to be called with f.pos at the tip of the previous peak,
// and will leave f.pos at the tip of the next peak (if there is one),
// or in the noise after the peak if it was the last one.
func (f *DCOffset) nextPeak() error {
	pw, data := f.PeakWidth, f.data

	// Find the end of the previous peak, and the start of the current.
	// The first time through (from firstPeak), this always exists, but
	// on later repetitions it might not, if the previously current peak
	// was the last one in this sequence.
	prev := f.findPeakAt(f.pos)
	log.F(4, "Previous peak: %+v\n", prev)
	if prev.End < 0 {
		// TODO: handle this somehow? (I'm not sure it can happen)
		return fmt.Errorf("previous peak too long at %v", prev.Start)
	}
	if prev.Next >= len(data) {
		// This peak went off the end of the data.
		// There's not much we can do here, so just apply the offset.
		log.Warn("peak runs off end of data at", prev.Start)
		f.applyOffsetUntil(len(data))
		return nil
	}
	if f.withinNoise(prev.Next) {
		// That was the last peak of this sequence, so end the sequence.
		to := min(prev.Next+pw, len(data))
		lo, hi := lowHigh(data[prev.Next:to])
		nextOffset := (lo + hi) / 2
		f.handleTrailingEdge(prev, nextOffset)
		return nil
	}

	// We have a current peak, so find its details, and look for a next.
	cur := f.findPeakAt(prev.Next)
	log.F(4, "Current peak: %+v\n", cur)
	if cur.End < 0 {
		// TODO: handle this somehow?
		return fmt.Errorf("current peak too long at %v", cur.Start)
	}
	if cur.Next >= len(data) {
		// This peak went off the end of the data.
		// There's not much we can do here, so just apply the offset.
		log.Warn("peak runs off end of data at", prev.Start)
		f.applyOffsetUntil(len(data))
		return nil
	}

	peakOffset := (prev.Value + cur.Value) / 2

	// Update the noise level before looking for the third peak.
	f.updateNoiseLevel(peakOffset, prev.Value, cur.Value)

	// TODO: enable or remove this code. Not sure the results are good.
	if false && f.outsideNoise(cur.Next) {
		// There is at least one more peak in this sequence, which must
		// be the same polarity as the previous peak. To smooth things
		// out a little, average its value with the previous peak.
		next := f.findPeakAt(cur.Next)
		log.F(4, "Next peak: %+v\n", next)
		if next.End < 0 {
			// TODO: handle this somehow?
			err := fmt.Errorf("next peak too long at %v", next.Start)
			return err
		}
		// If the peak goes off the end of the data, we can't really use
		// it safely, so just ignore it. Otherwise, add in its value.
		if next.Next < len(data) {
			prevNextAvg := (prev.Value + next.Value) / 2
			peakOffset = (prevNextAvg + cur.Value) / 2
			// TODO: do I want this? I don't think so, but not sure
			//f.setNoiseLevel(peakOffset, prevNextValue, cur.Value)
		}
	}

	// Apply the offset to the edge leading to this peak.
	// TODO: should I fade the old offset into the new one somehow?
	f.offset = peakOffset
	f.applyOffsetUntil(cur.Index)

	return nil
}

func (f *DCOffset) updateNoiseLevel(offset, tip1, tip2 int) {
	// The peak tips should be equally far from the offset, under normal
	// conditions, but if the offset is done differently, or the integer
	// math interferes, they might not be. Therefore, use the smaller of
	// the two to calculate the noise level.
	tipLevel := min(abs(tip1-offset), abs(tip2-offset))
	f.noiseLevel = max(f.NoiseFloor, tipLevel/10)
}

func (f *DCOffset) applyOffsetUntil(end int) {
	data, out, pos, offset := f.data, f.out, f.pos, f.offset
	for pos < end {
		out[pos] = data[pos] - offset
		pos++
	}
	f.pos = pos
}

type Peak struct {
	Value int // Value of the peak's tip
	Index int // Index of the peak's tip
	Start int // The index of the first non-noise sample of this peak
	End   int // The index of the last non-noise sample of this peak
	Next  int // The index that the next peak (or noise area) starts at
}

func (f *DCOffset) findPeakAt(start int) Peak {
	if f.data[start]-f.offset < 0 {
		return f.findLowPeak(start)
	} else {
		return f.findHighPeak(start)
	}
}

func (f *DCOffset) findLowPeak(start int) Peak {
	pw, nf, data, offset := f.PeakWidth, f.noiseLevel, f.data, f.offset
	p := start
	peak := Peak{
		Value: data[p],
		Index: p,
		Start: start,
		End:   p,
	}
	stop := start + pw*6
	for stop > 0 && p < len(data) && data[p]-offset <= nf {
		if data[p] < peak.Value {
			peak.Value = data[p]
			peak.Index = p
		}
		if data[p]-offset < -nf {
			peak.End = p
		} else if p-peak.End > pw {
			// Full peak width of noise, so this was the last peak.
			peak.Next = peak.End + 1
			return peak
		}
		p++
		stop--
	}
	if stop <= 0 {
		peak.End = -1
	}
	peak.Next = p
	return peak
}

func (f *DCOffset) findHighPeak(start int) Peak {
	pw, nf, data, offset := f.PeakWidth, f.noiseLevel, f.data, f.offset
	p := start
	peak := Peak{
		Value: data[p],
		Index: p,
		Start: start,
		End:   p,
	}
	stop := start + pw*6
	for stop > 0 && p < len(data) && data[p]-offset >= -nf {
		if data[p] > peak.Value {
			peak.Value = data[p]
			peak.Index = p
		}
		if data[p]-offset > nf {
			peak.End = p
		} else if p-peak.End > pw {
			// Full peak width of noise, so this was the last peak.
			peak.Next = peak.End + 1
			return peak
		}
		p++
		stop--
	}
	if stop <= 0 {
		peak.End = -1
	}
	peak.Next = p
	return peak
}
