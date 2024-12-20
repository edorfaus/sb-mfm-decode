package mfm

import (
	"fmt"
)

type PulseClass uint8

const (
	// PulseUnknown is only used if the pulse could not be classified.
	PulseUnknown PulseClass = iota
	// PulseTiny is any pulse that is too short to be PulseShort.
	PulseTiny
	PulseShort
	PulseMedium
	PulseLong
	// PulseHuge is any pulse that is too long to be PulseLong.
	PulseHuge
)

type PulseClassifier struct {
	Edges *EdgeDetect

	// The expected/detected width of an MFM data bit (aka short pulse).
	// This is updated automatically, based on the pulses seen so far.
	BitWidth float64

	// The class of the current pulse.
	Class PulseClass

	// The width in samples of the current pulse.
	Width float64

	// List of bit recent widths, used to calculate the current width.
	BitWidths []float64

	// The index into BitWidths that we are overwriting next.
	BWIndex int

	// The sum of the values currently in the BitWidths slice.
	BWTotal float64
}

func NewPulseClassifier(ed *EdgeDetect) *PulseClassifier {
	return &PulseClassifier{
		Edges: ed,

		BitWidths: make([]float64, 0, 16),
	}
}

func (c *PulseClassifier) Next() bool {
	if !c.Edges.Next() {
		return false
	}

	c.Width = c.Edges.CurZero - c.Edges.PrevZero

	if c.BitWidth == 0 {
		// When the bit width is not set, the data must start with a
		// lead-in, which can then be used to figure out the bit width.
		if !c.peekAtLeadIn() {
			c.Class = PulseUnknown
			return true
		}
	}

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
	// For comparisons, we use the fact that t < w*a/b => t*b < w*a
	// (given b>0) to avoid needing to do divisions.

	pulseWidth, bitWidth := c.Width, c.BitWidth

	switch {
	case pulseWidth*4 < bitWidth*3:
		// less than 2 half-bit widths
		c.Class = PulseTiny
	case pulseWidth*4 < bitWidth*5:
		// 2 half-bit widths
		c.Class = PulseShort
		c.addBitWidth(pulseWidth)
	case pulseWidth*4 < bitWidth*7:
		// 3 half-bit widths
		c.Class = PulseMedium
		c.addBitWidth(pulseWidth * 2 / 3)
	case pulseWidth*4 < bitWidth*9:
		// 4 half-bit widths
		c.Class = PulseLong
		c.addBitWidth(pulseWidth / 2)
	default:
		// more than 4 half-bit widths
		c.Class = PulseHuge
	}

	return true
}

// TouchesNone returns true if either edge of the pulse is EdgeToNone.
func (c *PulseClassifier) TouchesNone() bool {
	return c.Edges.PrevType == EdgeToNone ||
		c.Edges.CurType == EdgeToNone
}

// SetBitWidth sets the bit width in samples for the input edges.
//
// It also updates the underlying edge detector's settings accordingly.
//
// Calling this before starting to classify data is optional, but makes
// it possible to classify data that does not have an initial lead-in.
func (c *PulseClassifier) SetBitWidth(bitWidth float64) {
	if bitWidth < 2 {
		panic(fmt.Errorf("invalid bit width: %v", bitWidth))
	}

	// Reset the bit widths slice, and override it with the given value.
	c.BitWidths = c.BitWidths[:cap(c.BitWidths)]
	for i := 0; i < len(c.BitWidths); i++ {
		c.BitWidths[i] = bitWidth
	}
	c.BWTotal = bitWidth * float64(len(c.BitWidths))
	c.BWIndex = 0

	c.BitWidth = bitWidth

	c.updateCrossingTime(bitWidth)
}

func (c *PulseClassifier) addBitWidth(bitWidth float64) {
	bws := c.BitWidths
	if len(bws) < cap(bws) {
		c.BWTotal += bitWidth
		c.BitWidths = append(bws, bitWidth)
	} else {
		c.BWTotal = c.BWTotal - bws[c.BWIndex] + bitWidth
		bws[c.BWIndex] = bitWidth
		c.BWIndex++
		if c.BWIndex >= len(bws) {
			c.BWIndex = 0
		}
	}

	c.BitWidth = c.BWTotal / float64(len(c.BitWidths))

	c.updateCrossingTime(bitWidth)
}

func (c *PulseClassifier) updateCrossingTime(bitWidth float64) {
	// TODO: figure out what would be a good value for this
	c.Edges.MaxCrossingTime = int(bitWidth + 0.5)
}

// peekAtLeadIn is called when the BitWidth is 0, to peek ahead at the
// lead-in and use it to figure out the bit width to use.
// It returns false if it was unable to figure out the bit width.
func (c *PulseClassifier) peekAtLeadIn() bool {
	// The lead-in is a sequence of zero bits (short pulses), which can
	// be seen as a sequence of equidistant edges. To peek ahead at
	// those edges without consuming them, we make a backup copy of the
	// edge detector and restore it afterwards.
	edgesBackup := *c.Edges
	defer func() {
		*c.Edges = edgesBackup
	}()

	if c.Edges.PrevType == EdgeToNone {
		// This is (probably) the empty area before the first pulse.

		if c.Edges.MaxCrossingTime == 0 {
			// Just to have something to work with; changed later.
			width := ExpectedBitWidth(DefaultBitRate, 44100)
			c.updateCrossingTime(width)
		}

		if !c.Edges.Next() {
			return false
		}

		// Since the max crossing time might be wrong, use this pulse to
		// set it and then re-do the edge, in case its width changes.
		width := c.Edges.CurZero - c.Edges.PrevZero

		*c.Edges = edgesBackup
		c.updateCrossingTime(width)

		if !c.Edges.Next() {
			return false
		}
	}

	// We want to look at more than one pulse, since some of the early
	// ones are often distorted and the timing is often a fractional
	// number of samples.

	// TODO: adjust this to make use of the BitWidths functionality, and
	// to keep more than just the final average value.

	total := 0.0
	count := 0
	for {
		if c.TouchesNone() {
			// ToNone pulses are not reliable for timing, and indicate
			// that there aren't really (enough) proper pulses here.
			return false
		}

		width := c.Edges.CurZero - c.Edges.PrevZero

		total += width
		count++
		if count >= 8 {
			break
		}

		c.updateCrossingTime(total / float64(count))
		if !c.Edges.Next() {
			return false
		}
	}

	// Breaking out of the loop indicates we have enough pulses for now,
	// so average them and use that as the bit width.
	c.SetBitWidth(total / float64(count))

	// Copy the crossing time to the backup so it works after restore.
	edgesBackup.MaxCrossingTime = c.Edges.MaxCrossingTime

	return true
}

func (c PulseClass) Valid() bool {
	return c == PulseShort || c == PulseMedium || c == PulseLong
}

func (c PulseClass) String() string {
	const classes = "UTSMLH"
	if int(c) >= len(classes) {
		return fmt.Sprintf("[bad PulseClass=%d]", int(c))
	}
	return classes[c : c+1]
}
