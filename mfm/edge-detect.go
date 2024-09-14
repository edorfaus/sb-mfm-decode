package mfm

type EdgeType int

const (
	EdgeToNone EdgeType = iota
	EdgeToHigh
	EdgeToLow
)

// TODO: handle DC offsets
// TODO: add minimum pulse length or something to avoid glitches?

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
	i := e.CurIndex
	s := e.Samples
	noise := e.NoiseFloor
	if e.CurType == EdgeToNone {
		// Previous was none, so find either high or low.
		for i < len(s) && s[i] <= noise && s[i] >= -noise {
			i++
		}
		// TODO: check if it immediately drops back into noise (glitch)?
		// (even if only to match the behaviour when going into noise.)
		// TODO: look backwards for the point where it started to rise?
		// (to match detection of zero-crossing, if/when that is added.)
		e.CurIndex = i
		if i >= len(s) {
			e.CurType = EdgeToNone
			return false
		}
		if s[i] > noise {
			e.CurType = EdgeToHigh
		} else {
			e.CurType = EdgeToLow
		}
		return true
	}

	// Previous was high or low, so find either the opposite or none.
	if i >= len(s) {
		return false
	}

	// Look for the first non-noise sample on the other side of 0.
	// Note that this ignores dips into noise that come back out on the
	// same side as before, unless one is long enough to be EdgeToNone.

	maxTime := e.MaxCrossingTime
	t := maxTime

	if s[i] < 0 {
		// We are low, so look for an edge to high (or none).
		for i++; i < len(s) && s[i] <= noise; i++ {
			// Check for too many within-noise samples.
			if s[i] < -noise {
				t = maxTime
			} else {
				t--
				if t < 0 {
					// Too many within-noise, this is an edge to none.
					// TODO: look back for the first nearest-0 point?
					e.CurType = EdgeToNone
					e.CurIndex = i
					return true
				}
			}
		}
		e.CurIndex = i
		if i >= len(s) {
			// No edge was found.
			// TODO: check for a short ToNone edge? do we even want it?
			return false
		}
		// TODO: look backwards for the point where it crosses zero?
		e.CurType = EdgeToHigh
		return true
	}

	// We are high, so look for an edge to low (or none).
	for i++; i < len(s) && s[i] >= -noise; i++ {
		// Check for too many within-noise samples.
		if s[i] > noise {
			t = maxTime
		} else {
			t--
			if t < 0 {
				// Too many within-noise, this is an edge to none.
				// TODO: look back for the first nearest-0 point?
				e.CurType = EdgeToNone
				e.CurIndex = i
				return true
			}
		}
	}
	e.CurIndex = i
	if i >= len(s) {
		// No edge was found.
		// TODO: check for a short ToNone edge? do we even want it?
		return false
	}
	// TODO: look backwards for the point where it crosses zero?
	e.CurType = EdgeToLow
	return true
}
