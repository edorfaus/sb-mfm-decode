package filter

import (
	"golang.org/x/exp/slices"
)

func DefaultNoiseFloor(bits int) int {
	maxValue := 1 << (bits - 1)
	return maxValue * 2 / 100
}

func MfmPeakWidth(mfmBitRate, sampleRate int) int {
	// ceil(sampleRate / mfmBitRate)
	return (sampleRate + mfmBitRate - 1) / mfmBitRate
}

func lowHigh(v []int) (low, high int) {
	return slices.Min(v), slices.Max(v)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
