package mfm

import (
	"fmt"
)

const DefaultBitRate = 4800

var EOD = fmt.Errorf("end of input data")

type Decoder struct {
	Edge *EdgeDetect

	// Width of the latest data bit (two half-bits).
	// This should not be set directly, use SetBitWidth() instead.
	BitWidth int

	// The start and end sample index of the current block of bits.
	StartIndex int
	EndIndex   int

	// The bits of the current MFM block - both clock and data bits.
	Bits []byte
}

func NewDecoder(ed *EdgeDetect) *Decoder {
	d := &Decoder{
		Edge: ed,
	}
	return d
}

// InitFreq initializes the decoder based on the given MFM bitrate and
// input sample rate. This is optional, but makes it possible to decode
// data without an initial lead-in.
func (d *Decoder) InitFreq(mfmBitRate, sampleRate int) {
	if mfmBitRate == 0 {
		mfmBitRate = DefaultBitRate
	} else if mfmBitRate < 0 {
		panic("invalid MFM bit rate")
	}
	// While more is preferred, minimum 2x bit rate is needed (Nyquist).
	if sampleRate < 2*mfmBitRate {
		panic("invalid sample rate: must be at least 2 * bit rate")
	}
	samplesPerCycle := sampleRate / mfmBitRate
	//samplesPerPeak := samplesPerCycle / 2
	d.SetBitWidth(samplesPerCycle)
}

// SetBitWidth sets the bit width in samples for the input edges.
//
// It also updates the underlying edge detector's settings accordingly.
func (d *Decoder) SetBitWidth(bitWidth int) {
	if bitWidth < 2 {
		panic(fmt.Errorf("invalid bit width: %v", bitWidth))
	}
	// TODO: should we use a weighted average of recent bit widths?
	// If so, should we change it to be a float, for higher precision?
	// If so, we might need another float field for current position.
	d.BitWidth = bitWidth
	// TODO: figure out what would be a good value for this
	d.Edge.MaxCrossingTime = bitWidth / 2
}

func (d *Decoder) NextBlock() error {
	if d.Edge.CurType != EdgeToNone {
		return fmt.Errorf("edge detector in bad state for next block")
	}

	d.Bits = d.Bits[:0]

	defer func() {
		d.EndIndex = d.Edge.CurIndex
	}()

	if !d.Edge.Next() {
		d.StartIndex = d.Edge.PrevIndex
		return EOD
	}

	// At this point, the previous edge is ToNone, the current is not.
	// (Assuming the edge detector is functioning correctly.)

	d.StartIndex = d.Edge.CurIndex

	// In MFM encoding, the distance between edges is either 2, 3 or 4
	// half-bit-widths. Both tape speed variability and the likely
	// mismatch between the sampling rate and the MFM bitrate mean that
	// we can't expect the bit widths to be exact, but have to check
	// which of those any particular edge distance is closest to.
	// Therefore, we want to compare against points halfway between the
	// expected bit-widths, to better classify them.
	// Thus, if w is the half-bit-width, then the target points are at
	// 2*w, 3*w, and 4*w; and the split points between them are at
	// (2*w+3*w)/2 = w*(2+3)/2 = w*5/2, and at (3*w+4*w)/2 = w*7/2.
	//
	// However, we are actually measuring data-bit-widths (2 half-bits)
	// since that is easier to do (in part due to the lead-in).
	// Thus, if w is data-bit-width, the target points are actually at
	// w*2/2, w*3/2, and w*4/2, and the split points at w*5/4 and w*7/4.
	//
	// For error checking, we also want to look for pulses that are too
	// long or too short; while we could be more lenient with those as
	// there's no neighboring group being encroached on, we are keeping
	// the groups the same size, placing the cut-off points accordingly.
	// Thus, at (w*1/2+w*2/2)/2 = w*(1/2+2/2)/2 = w*(3/2)/2 = w*3/4
	// and at   (w*4/2+w*5/2)/2 = w*(4/2+5/2)/2 = w*(9/2)/2 = w*9/4
	//
	// For comparisons, we use the fact that t < w*5/4 => t*4 < w*5,
	// to avoid the precision loss of the integer division.

	if d.BitWidth == 0 {
		// We don't have any data about the bit-width, so a lead-in is
		// required, to figure out what the bit-width should be. That
		// lead-in must start with at least one 0-bit, so grab it and
		// use its timing as the initial bit width.
		if !d.Edge.Next() {
			// The data only contains one edge; there's no data here.
			return fmt.Errorf("bad data: only one edge before EOD")
		}
		d.SetBitWidth(d.Edge.CurIndex - d.Edge.PrevIndex)
		d.Bits = append(d.Bits, 1, 0)
	}

	prevBit := byte(0)
	// TODO: should the last edge (to none) be included in the data?
	for d.Edge.CurType != EdgeToNone && d.Edge.Next() {
		delta := d.Edge.CurIndex - d.Edge.PrevIndex
		// The below comparisons all use delta*4, so do that only once.
		switch {
		case delta*4 < d.BitWidth*3:
			// TODO: do I want to handle glitches here or in EdgeDetect?
			return fmt.Errorf(
				"bad data: edge distance too short: delta %v, bw %v",
				delta, d.BitWidth,
			)
		case delta*4 < d.BitWidth*5:
			// 2 half-bit widths: same data bit as previous
			d.Bits = append(d.Bits, 1-prevBit, prevBit)
			d.SetBitWidth(delta)
		case delta*4 < d.BitWidth*7:
			// 3 half-bit widths
			if prevBit == 0 {
				d.Bits = append(d.Bits, 1, 0, 0, 1)
				prevBit = 1
			} else {
				d.Bits = append(d.Bits, 0, 0)
				prevBit = 0
			}
			d.SetBitWidth(delta * 2 / 3)
		case delta*4 < d.BitWidth*9:
			// 4 half-bit widths
			// This only happens when the previous bit was 1, and the
			// next data is a 0 followed by a 1.
			if prevBit != 1 {
				return fmt.Errorf(
					"bad data: delta too large after 0: %v, with bw %v",
					delta, d.BitWidth,
				)
			}
			d.Bits = append(d.Bits, 0, 0, 0, 1)
			d.SetBitWidth(delta / 2)
		default:
			return fmt.Errorf(
				"bad data: edge distance too long: delta %v, bw %v",
				delta, d.BitWidth,
			)
		}
	}

	if d.Edge.CurType != EdgeToNone {
		// This means d.Edge.Next() returned false, which means we're at
		// the end of the input data. If we do not have any bits, that
		// means we only got the first edge, which suggests a problem.
		if len(d.Bits) == 0 {
			return fmt.Errorf("bad data: only one edge before EOD")
		}
		return EOD
	}

	return nil
}
