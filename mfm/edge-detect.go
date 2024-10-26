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
	// The interpolated sample offset of the current edge.
	CurZero float64

	// The index (in samples) and type of the previous edge.
	PrevIndex int
	PrevType  EdgeType
	// The interpolated sample offset of the previous edge.
	PrevZero float64
}

func NewEdgeDetect(samples []int, noiseFloor int) *EdgeDetect {
	return &EdgeDetect{
		Samples:    samples,
		NoiseFloor: noiseFloor,
	}
}

func (e *EdgeDetect) Next() bool {
	e.PrevIndex, e.PrevType = e.CurIndex, e.CurType
	e.PrevZero = e.CurZero

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
		e.CurZero = float64(i)
		return false
	}

	if s[i] > noise {
		e.CurType = EdgeToHigh
	} else {
		e.CurType = EdgeToLow
	}

	if i <= 0 {
		// Immediate edge at the start of the data, so there's no better
		// index to place it at.
		e.CurZero = float64(i)
		return true
	}

	// There were within-noise samples before the peak, so look back to
	// find a good spot to say that the edge happened.

	// Do not go further back than the max crossing time, since we do
	// not go further ahead than it on edges to none either.
	from := i - e.MaxCrossingTime
	// Also, do not go all the way back to the previous edge.
	if from <= e.PrevIndex {
		from = e.PrevIndex + 1
	}

	// First: use the noise-crossing values to extrapolate where a line
	// that continues straight would cross zero.
	// TODO: consider using a higher peak-relative (say 10%) noise level
	// for this? But we'd have to support using that elsewhere too, and
	// would need to look ahead to find the tip of this first peak.
	zc := float64(i) - 1 + intersectXAxis(s[i-1], s[i])
	// Clamp it to the valid area
	if zc > float64(i) {
		zc = float64(i)
	}
	if zc < float64(from) {
		zc = float64(from)
	}

	end := int(0.5 + zc)

	// Next: in the area around that extrapolated zero-crossing, look
	// for an actual zero-crossing, since the cleanup often makes one.
	until := end - (i - end)
	if until < from {
		until = from
	}
	if e.CurType == EdgeToHigh {
		for j := i - 1; j >= until; j-- {
			if s[j] <= 0 {
				end = j + 1
				zc = float64(j) + intersectXAxis(s[j], s[j+1])
				break
			}
		}
	} else {
		for j := i - 1; j >= until; j-- {
			if s[j] >= 0 {
				end = j + 1
				zc = float64(j) + intersectXAxis(s[j], s[j+1])
				break
			}
		}
	}

	e.CurIndex = end
	e.CurZero = zc
	return true
}

// nextFromLow is called by Next to find a high (or none) from a low.
func (e *EdgeDetect) nextFromLow() bool {
	i, s, noise := e.CurIndex, e.Samples, e.NoiseFloor
	maxTime := e.MaxCrossingTime

	// Look for the first non-noise sample on the other side of zero.
	// Note that this ignores dips into noise that come back out on the
	// same side as before, unless one is long enough to be EdgeToNone.
	ld := i
	for i++; i < len(s) && s[i] <= noise && i-ld <= maxTime; i++ {
		if s[i] < -noise {
			ld = i
		}
	}

	if i < len(s) && s[i] > noise {
		// We found an edge to high.
		// Look backwards for the point where it crosses zero
		for i--; s[i] > 0; {
			i--
		}
		e.CurIndex = i + 1
		e.CurType = EdgeToHigh
		e.CurZero = float64(i) + intersectXAxis(s[i], s[i+1])
		return true
	}

	// No edge was found before the end, or there were too many
	// consecutive within-noise samples, so this is an edge to none.
	e.CurType = EdgeToNone

	if ld+1 >= len(s) {
		// The last data was at the end, so the edge is at the end too.
		e.CurIndex = len(s)
		e.CurZero = float64(len(s))
		return true
	}

	// There were within-noise samples after the peak, so look back to
	// find a good spot to say that the edge happened.

	// First: use the noise-crossing values to extrapolate where a line
	// that continues straight (instead of fading out) would cross zero.
	// TODO: consider using a higher peak-relative (say 10%) noise level
	// for this? But we'd have to support using it in nextFromNone too.
	zc := float64(ld) + intersectXAxis(s[ld], s[ld+1])
	// Clamp it to the valid area
	if zc > float64(i) {
		zc = float64(i)
	}
	if zc < float64(ld) {
		zc = float64(ld)
	}

	end := int(0.5 + zc)

	// Next: in the area around that extrapolated zero-crossing, look
	// for an actual zero-crossing, just in case there is one.
	last := end + (end - ld)
	if last > i {
		last = i
	}
	for j := ld + 1; j < last; j++ {
		if s[j] >= 0 {
			end = j
			zc = float64(j) - 1 + intersectXAxis(s[j-1], s[j])
			break
		}
	}

	e.CurIndex = end
	e.CurZero = zc
	return true
}

// nextFromHigh is called by Next to find a low (or none) from a high.
func (e *EdgeDetect) nextFromHigh() bool {
	i, s, noise := e.CurIndex, e.Samples, e.NoiseFloor
	maxTime := e.MaxCrossingTime

	// Look for the first non-noise sample on the other side of zero.
	// Note that this ignores dips into noise that come back out on the
	// same side as before, unless one is long enough to be EdgeToNone.
	ld := i
	for i++; i < len(s) && s[i] >= -noise && i-ld <= maxTime; i++ {
		if s[i] > noise {
			ld = i
		}
	}

	if i < len(s) && s[i] < -noise {
		// We found an edge to low.
		// Look backwards for the point where it crosses zero.
		for i--; s[i] < 0; {
			i--
		}
		e.CurIndex = i + 1
		e.CurType = EdgeToLow
		e.CurZero = float64(i) + intersectXAxis(s[i], s[i+1])
		return true
	}

	// No edge was found before the end, or there were too many
	// consecutive within-noise samples, so this is an edge to none.
	e.CurType = EdgeToNone

	if ld+1 >= len(s) {
		// The last data was at the end, so the edge is at the end too.
		e.CurIndex = len(s)
		e.CurZero = float64(len(s))
		return true
	}

	// There were within-noise samples after the peak, so look back to
	// find a good spot to say that the edge happened.

	// First: use the noise-crossing values to extrapolate where a line
	// that continues straight (instead of fading out) would cross zero.
	// TODO: consider using a higher peak-relative (say 10%) noise level
	// for this? But we'd have to support using it in nextFromNone too.
	zc := float64(ld) + intersectXAxis(s[ld], s[ld+1])
	// Clamp it to the valid area
	if zc > float64(i) {
		zc = float64(i)
	}
	if zc < float64(ld) {
		zc = float64(ld)
	}

	end := int(0.5 + zc)

	// Next: in the area around that extrapolated zero-crossing, look
	// for an actual zero-crossing, just in case there is one.
	last := end + (end - ld)
	if last > i {
		last = i
	}
	for j := ld + 1; j < last; j++ {
		// For this, we consider exactly zero to be a crossing point.
		if s[j] <= 0 {
			end = j
			zc = float64(j) - 1 + intersectXAxis(s[j-1], s[j])
			break
		}
	}

	e.CurIndex = end
	e.CurZero = zc
	return true
}

// intersectXAxis calculates where the given line intersects the X axis.
// The line is given as the Y values of two points that are assumed to
// be 1 unit apart along the X axis. The returned value is the distance
// along the X axis to the intersection point from the first point.
func intersectXAxis(y1, y2 int) float64 {
	// Line 1: given: from x1,y1 to x2,y2 (where x2 = x1 + 1)
	// Line 2: X axis: from x3,y3 = -inf,0 to x4,y4 = inf,0
	// To simplify, since we know what the second line is, we eliminate
	// x1 from line 1, and define line 2 with x3 = 0 and x4 = 1 instead.
	// This gives us x1 = x3 = y3 = y4 = 0 and x2 = x4 = 1.
	// We know that the intersection must happen at Y=0, so we only need
	// to find the X coordinate. Using the determinants, we have that:
	// X = (x1*y2-y1*x2)*(x3-x4) - (x1-x2)*(x3*y4-y3*x4)
	//  all over (x1-x2)*(y3-y4) - (y1-y2)*(x3-x4)
	// Inserting the known values:
	// X = (0*y2-y1*1)*(0-1) - (0-1)*(0*0-0*1)
	//  all over (0-1)*(0-0) - (y1-y2)*(0-1)
	// Simplifying the constants:
	// X = ( ((0 - y1)*-1) - (-1*(0 - 0)) ) / ( (-1*0) - (y1 - y2)*-1 )
	// X = ( (-y1 * -1) - 0 ) / ( 0 - -1 * (y1 - y2) )
	// X = ( -y1 * -1 ) / ( 0 - -(y1 - y2) )
	// X = ( y1 * 1 ) / ( 0 + (y1 - y2) ) = y1 / ( y1 - y2 )

	return float64(y1) / float64(y1-y2)
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
