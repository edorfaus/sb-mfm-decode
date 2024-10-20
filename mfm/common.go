package mfm

// DefaultBitRate is the default MFM bit rate, as used for the StudyBox.
const DefaultBitRate = 4800

// ExpectedBitWidth calculates the expected MFM bit width for the given
// MFM bit rate and input sampling rate.
func ExpectedBitWidth(mfmBitRate, sampleRate int) float64 {
	if mfmBitRate == 0 {
		mfmBitRate = DefaultBitRate
	}
	if mfmBitRate <= 0 {
		panic("invalid MFM bit rate")
	}
	// While more is preferred, minimum 2x bit rate is needed, because
	// we need to distinguish between pulse widths of 1, 1.5 and 2.
	if sampleRate < 2*mfmBitRate {
		panic("invalid sample rate: must be at least 2 * bit rate")
	}
	// This is my attempt at doing proper half-way rounding in int math.
	return float64(sampleRate) / float64(mfmBitRate)
}
