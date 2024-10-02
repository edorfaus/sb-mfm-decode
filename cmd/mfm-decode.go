package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/edorfaus/sb-mfm-decode/mfm"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	samples := buildSamples(
		1, 1, // leading none
		2, 2, 0, 0, 2, 2, 2, 0, 0, 2, 2, 0, 0, 2, 2,
		1, 1, // trailing none
	)
	fmt.Println("Samples:", len(samples))
	//fmt.Println("   ", samples)
	ed := mfm.NewEdgeDetect(samples, 32768*2/100)
	d := mfm.NewDecoder(ed)

	err := d.NextBlock()
	for ; err == nil; err = d.NextBlock() {
		if len(d.Bits) == 0 {
			fmt.Printf(
				"empty block: start %v, end %v, bit width %v: %v\n",
				d.StartIndex, d.EndIndex, d.BitWidth, d.Bits,
			)
			continue
		}
		bits, liErr := skipLeadIn(d.Bits)
		fmt.Printf(
			"block: start %v, end %v, bit width %v, lead-in %v: %v\n",
			d.StartIndex, d.EndIndex, d.BitWidth,
			len(d.Bits)-len(bits), bits,
		)
		//fmt.Println("  All bits:", d.Bits)
		if liErr != nil {
			fmt.Println("  Warning:", liErr)
		}
	}

	if len(d.Bits) != 0 && errors.Is(err, mfm.EOD) {
		// This should never happen, as long as the decoder works.
		err = fmt.Errorf("EOD block contains data")
	}
	if !errors.Is(err, mfm.EOD) {
		fmt.Printf(
			"failed block: start %v, end %v, bit width %v: %v\n",
			d.StartIndex, d.EndIndex, d.BitWidth, d.Bits,
		)
		return err
	}

	fmt.Printf(
		"EOD block: start %v, end %v, bit width %v\n",
		d.StartIndex, d.EndIndex, d.BitWidth,
	)
	return nil
}

func buildSamples(halfBits ...int) []int {
	const halfBitWidth = 4
	const scale = 16384
	out := make([]int, 0, len(halfBits)*halfBitWidth)
	for _, v := range halfBits {
		for i := 0; i < halfBitWidth; i++ {
			out = append(out, (v-1)*scale)
		}
	}
	return out
}

// In studybox, after the MFM lead-in (0s then 1), there's a 0-bit
// before each byte of data. So, more or less, each byte takes 9 bits.

func skipLeadIn(bits []byte) ([]byte, error) {
	// The lead-in is a data sequence of 0s followed by a single 1.
	// Adding the clock, each data bit gets expanded into 2 stored bits,
	// such that the lead-in becomes a sequence of 10 followed by a 01,
	// like this: 101010...101001.

	i := 0
	for i+1 < len(bits) && bits[i] == 1 && bits[i+1] == 0 {
		i += 2
	}

	if i == 0 {
		return bits, fmt.Errorf("lead-in: no lead-in found")
	}

	if i+1 >= len(bits) || bits[i] != 0 || bits[i+1] != 1 {
		return bits, fmt.Errorf("lead-in: end marker not found")
	}

	return bits[i+2:], nil
}
