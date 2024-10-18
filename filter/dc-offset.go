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
}

func NewDCOffset(noiseFloor, peakWidth int) *DCOffset {
	return &DCOffset{
		NoiseFloor: noiseFloor,
		PeakWidth:  peakWidth,
	}
}

func (f *DCOffset) Run(data []int) []int {
	if f.PeakWidth <= 0 {
		f.PeakWidth = 48000 / 4800
	}

	f.data = data
	f.offset = 0
	f.out = make([]int, len(f.data))
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
			log.Ln(0, "Error: firstPeak:", err)
			return f.out
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
				log.Ln(0, "Error: nextPeak:", err)
				return f.out
			}
		}
	}

	return f.out
}

func (f *DCOffset) outsideNoise(pos int) bool {
	data := f.data
	return pos < len(data) && abs(data[pos]-f.offset) > f.NoiseFloor
}

func (f *DCOffset) withinNoise(pos int) bool {
	data := f.data
	return pos < len(data) && abs(data[pos]-f.offset) <= f.NoiseFloor
}

// Move past the leading noise in the data, while adjusting the offset.
func (f *DCOffset) leadingNoise() {
	pw, nf, data := f.PeakWidth, f.NoiseFloor, f.data
	for f.pos < len(data) {
		to := min(f.pos+pw, len(data))
		lo, hi := lowHigh(data[f.pos:to])
		if abs(lo-f.offset) > nf || abs(hi-f.offset) > nf {
			// Found a peak.
			return
		}

		// No peak here, just noise, so adjust the offset by averaging
		// the old value with the new middle-point.
		f.offset = (f.offset + ((lo + hi) / 2)) / 2
		f.out[f.pos] = f.offset
		f.pos++
	}
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
		to := min(peak.Next+pw, len(data))
		lo, hi := lowHigh(data[peak.Next:to])
		nextOffset := (lo + hi) / 2
		peakOffset := (f.offset + nextOffset) / 2
		f.peakOffsetFadeIn(peak, peakOffset)
		f.peakOffsetFadeOut(peak, nextOffset)
		return nil
	}

	// We found the first peak, and the start of the second.
	// Find the rest of the second peak, to find the overall DC offset.

	nextPeak := f.findPeakAt(peak.Next)
	log.F(3, "Second peak: %+v\n", nextPeak)

	if nextPeak.End < 0 {
		//log.Warn("next peak too long at", nextPeak.Start)
		// TODO: handle this somehow?
		return fmt.Errorf("next peak too long at %v", nextPeak.Start)
	}

	f.peakOffsetFadeIn(peak, (peak.Value+nextPeak.Value)/2)

	return nil
}

// This applies the offset to the leading edge of the given peak (the
// noise before it, and fading in the new offset up to the peak's tip).
// It does not touch the sample that is at the peak's tip, but leaves
// the current position pointing at that sample, and sets f.offset.
// This is only intended to be used for the first peak in a group.
func (f *DCOffset) peakOffsetFadeIn(peak Peak, peakOffset int) {
	// TODO: consider adjusting the offset also between pos and start.
	f.applyOffsetUntil(peak.Start)

	// TODO: The below interpolation works, in that it does what it was
	// designed to do. However, looking at the output, I no longer think
	// this is something we *want* to do. (The peak becomes too long.)

	// Interpolate between current offset and the target peak offset
	// With t going from 0 to 1:
	// v = from*(1-t) + to*(t) = from-from*t + to*t = from+(to-from)*t
	// And, t = relT / deltaT = (pos-start) / (end-start)
	// However, we adjust things a bit, since we want to start the lerp
	// with the old offset at the sample _before_ the start of the peak.
	dT := peak.Index - f.pos + 1
	dOfs := peakOffset - f.offset
	for relT := 1; relT < dT; relT++ {
		// TODO: use proper rounding with this formula?
		f.out[f.pos] = f.offset + dOfs*relT/dT
		f.pos++
	}

	f.offset = peakOffset
}

// This applies the offset to the trailing edge of the given peak (the
// tip, and out to the noise following it), fading out the offset from
// the current value to the given next offset, and then sets f.offset.
// This is only intended to be used for the last peak in a group, and
// expects that the current position is at the tip of that peak.
func (f *DCOffset) peakOffsetFadeOut(peak Peak, nextOffset int) {
	// TODO: this is probably not what I want to do, see FadeIn above

	// Interpolate between current offset and the target peak offset
	// With t going from 0 to 1:
	// v = from*(1-t) + to*(t) = from-from*t + to*t = from+(to-from)*t
	// And, t = relT / deltaT = (pos-start) / (end-start)
	dT := peak.Next - f.pos
	dOfs := nextOffset - f.offset
	for relT := 0; relT < dT; relT++ {
		// TODO: use proper rounding with this formula?
		f.out[f.pos] = f.offset + dOfs*relT/dT
		f.pos++
	}

	f.offset = nextOffset
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
		f.peakOffsetFadeOut(prev, nextOffset)
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

	prevNextValue := prev.Value

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
			prevNextValue = (prevNextValue + next.Value) / 2
		}
	}

	peakOffset := (prevNextValue + cur.Value) / 2

	// Apply the offset to the edge leading to this peak.
	// TODO: should I fade the old offset into the new one somehow?
	f.offset = peakOffset
	f.applyOffsetUntil(cur.Index)

	return nil
}

func (f *DCOffset) applyOffsetUntil(end int) {
	for f.pos < end {
		f.out[f.pos] = f.offset
		f.pos++
	}
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
	pw, nf, data, offset := f.PeakWidth, f.NoiseFloor, f.data, f.offset
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
	pw, nf, data, offset := f.PeakWidth, f.NoiseFloor, f.data, f.offset
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
