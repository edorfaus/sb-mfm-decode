package mfm

type EdgeType int

const (
	EdgeToNone EdgeType = iota
	EdgeToHigh
	EdgeToLow
)

// This edge detector assumes that there is nothing outside of the given
// samples; that is, that both before and after the given samples, there
// is an infinitude of samples that are neither high nor low. This means
// that if the given samples have high or low values at either end, then
// that end will be considered to be an edge.

// TODO: on EdgeToNone, go back to 1st 0-pt: long tail edge = bad data

// TODO: add minimum pulse length or something to avoid glitches?
// TODO: add float interpolation of edge's zero crossing point?

type EdgeDetect struct {
	// The list of samples that this edge detector is finding edges in.
	Samples []int

	// The maximum absolute sample value that is considered to not be a
	// signal (meaning it is within the noise).
	NoiseFloor int

	// The maximum time (in samples) allowed for crossing the zero point
	// when switching from high to low (or vice versa); if it takes
	// longer than this, it is instead detected as an edge to none.
	MaxCrossingTime int

	// The index (in samples) and type of the current edge.
	CurIndex int
	CurType  EdgeType

	// The index (in samples) and type of the previous edge.
	PrevIndex int
	PrevType  EdgeType
}

func NewEdgeDetect(samples []int, noiseFloor int) *EdgeDetect {
	return &EdgeDetect{
		Samples:    samples,
		NoiseFloor: noiseFloor,
	}
}

func (e *EdgeDetect) Next() bool {
	e.PrevIndex, e.PrevType = e.CurIndex, e.CurType

	if e.CurIndex >= len(e.Samples) {
		// We are already past the end of the data, so there are no more
		// edges to be found.
		e.CurType = EdgeToNone
		return false
	}

	switch e.CurType {
	case EdgeToNone:
		return e.nextFromNone()
	case EdgeToLow:
		return e.nextFromLow()
	case EdgeToHigh:
		return e.nextFromHigh()
	}

	panic("bad state: unknown value in EdgeDetect.CurType")
}

// nextFromNone is called by Next to find an edge (or EOD) from a none.
func (e *EdgeDetect) nextFromNone() bool {
	i, s, noise := e.CurIndex, e.Samples, e.NoiseFloor

	// Look for the first non-noise sample on either side of zero.
	for i < len(s) && s[i] <= noise && s[i] >= -noise {
		i++
	}
	// TODO: check if it immediately drops back into noise (glitch)?
	// (even if only to match the behaviour when going into noise.)

	e.CurIndex = i
	if i >= len(s) {
		e.CurType = EdgeToNone
		return false
	}

	// TODO: look backwards for the point where it started to rise
	// (to match detection of zero-crossing, when that is added.)

	if s[i] > noise {
		e.CurType = EdgeToHigh
	} else {
		e.CurType = EdgeToLow
	}
	return true
}

// nextFromLow is called by Next to find a high (or none) from a low.
func (e *EdgeDetect) nextFromLow() bool {
	i, s, noise := e.CurIndex, e.Samples, e.NoiseFloor
	maxTime := e.MaxCrossingTime
	t := maxTime

	// Look for the first non-noise sample on the other side of zero.
	// Note that this ignores dips into noise that come back out on the
	// same side as before, unless one is long enough to be EdgeToNone.
	for i++; i < len(s) && s[i] <= noise; i++ {
		// Check for too many consecutive within-noise samples.
		if s[i] < -noise {
			t = maxTime
		} else {
			t--
			if t < 0 {
				break
			}
		}
	}

	if i >= len(s) || t < 0 {
		// No edge was found before the end, or there were too many
		// consecutive within-noise samples, so this is an edge to none.
		e.CurType = EdgeToNone
		if t != maxTime {
			// The previous sample was within the noise, so look back
			// for the first nearest-0 point, to place the edge there.
			// TODO: implement the above comment
		}
		e.CurIndex = i
		return true
	}

	// Look backwards for the point where it crosses zero
	for i--; s[i] > 0; {
		i--
	}
	e.CurIndex = i + 1
	e.CurType = EdgeToHigh
	return true
}

// nextFromHigh is called by Next to find a low (or none) from a high.
func (e *EdgeDetect) nextFromHigh() bool {
	i, s, noise := e.CurIndex, e.Samples, e.NoiseFloor
	maxTime := e.MaxCrossingTime
	t := maxTime

	// Look for the first non-noise sample on the other side of zero.
	// Note that this ignores dips into noise that come back out on the
	// same side as before, unless one is long enough to be EdgeToNone.
	for i++; i < len(s) && s[i] >= -noise; i++ {
		// Check for too many consecutive within-noise samples.
		if s[i] > noise {
			t = maxTime
		} else {
			t--
			if t < 0 {
				break
			}
		}
	}

	if i >= len(s) || t < 0 {
		// No edge was found before the end, or there were too many
		// consecutive within-noise samples, so this is an edge to none.
		e.CurType = EdgeToNone
		if t != maxTime {
			// The previous sample was within the noise, so look back
			// for the first nearest-0 point, to place the edge there.
			// TODO: implement the above comment
		}
		e.CurIndex = i
		return true
	}

	// Look backwards for the point where it crosses zero
	for i--; s[i] < 0; {
		i--
	}
	e.CurIndex = i + 1
	e.CurType = EdgeToLow
	return true
}

func (t EdgeType) String() string {
	switch t {
	case EdgeToNone:
		return "N"
	case EdgeToHigh:
		return "H"
	case EdgeToLow:
		return "L"
	default:
		return "?"
	}
}
