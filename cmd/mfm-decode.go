package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	samples := []int{0, 1000, 1000, -1000}
	edges := EdgeDetect(samples, 32768*2/100)
	fmt.Println("edges:", edges)
	d := &MfmDecoder{Edges: edges}
	if err := d.DecodeChunk(); err != nil {
		return err
	}
	fmt.Println("bits:", d.Bits)
	return nil
}

// In studybox, after the MFM lead-in (0s then 1), there's a 0-bit
// before each byte of data. So, more or less, each byte takes 9 bits.

type MfmDecoder struct {
	Edges   []int
	curEdge int

	// bitSize is the sample size of each bit.
	// TODO: replace this with an array to get a rolling average, to
	// better handle variation due to sample rate not matching the MFM.
	bitSize int

	// Bits is the bits in the currently decoded chunk.
	Bits []byte
}

func (d *MfmDecoder) DecodeChunk() error {
	if err := d.handleLeadIn(); err != nil {
		return fmt.Errorf("MFM lead-in: %w", err)
	}
	// d.curEdge is now pointing to the 1-bit at the end of the lead-in.
	if err := d.decodeData(); err != nil {
		return fmt.Errorf("MFM data: %w", err)
	}
	return nil
}

func (d *MfmDecoder) decodeData() error {
	edges := d.Edges
	bitSize := d.bitSize
	bits := d.Bits[:0]
	prevBit := 0
	for i := d.curEdge + 1; i < len(edges); i++ {
		dist := edges[i] - edges[i-1]
		// The distance between edges should be 2, 3 or 4 times bitSize.
		// If it is significantly larger, that means we hit the end.
		// So, try to classify the distance into one of those buckets,
		// while allowing some variance in the actual distance.
		// We can't just use a simple divide since we want some rounding
		// and accept larger buckets at either end.
		// We use this equiv: a > b*2.5 => a > b*25/10 => a * 10 > b*25
		var group int
		switch {
		case dist*10 > bitSize*45:
			// It's larger than 4, check if it's large enough that we
			// consider it to be the end of the data.
			if dist < bitSize*10 {
				return fmt.Errorf("edge distance too large before EOD")
			}
			// End of data found
			d.Bits = bits
			d.bitSize = bitSize
			d.curEdge = i
			return nil
		case dist*10 > bitSize*35:
			group = 4
		case dist*10 > bitSize*25:
			group = 3
		case dist*10 > bitSize*15:
			group = 2
		default:
			return fmt.Errorf("edge distance too short")
		}
		if prevBit == 0 {
			// TODO
			switch group {
			case 2:
			case 3:
			case 4:
			}
		} else {
			// prevBit == 1
			// TODO
			switch group {
			case 2:
			case 3:
			case 4:
			}
		}
	}
	return fmt.Errorf("ran off the end of the edges")
}

func (d *MfmDecoder) handleLeadIn() error {
	edges := d.Edges
	if len(edges) < 2 {
		d.curEdge = len(edges)
		return fmt.Errorf("too few edges in input")
	}

	// The lead-in is a data sequence of 0s followed by a single 1.
	// Adding the clock, each data bit gets expanded into 2 stored bits,
	// such that the lead-in becomes a sequence of 10 followed by a 01,
	// like this: 101010...101001. Each 1-bit is encoded as an edge,
	// while each 0-bit is encoded as no edge. Thus, for most of the
	// lead-in, there are exactly 2 bits between each pair of edges,
	// which allows us to calculate the size in samples of each bit.
	// Then, the end of the lead-in can be detected by there being two
	// edges with a distance of 3 bits.

	twoBitSize := edges[1] - edges[0]
	for i := 2; i < len(edges); i++ {
		size := edges[i] - edges[i-1]
		if size < twoBitSize {
			// TODO: handle glitches here? or earlier in edge detection?
			twoBitSize = size
			continue
		}
		if size > twoBitSize {
			// Check for end of lead-in (distance of 3 bits).
			// To allow for some variability in the sampling speed, we
			// set the cut-off point at 2.5 bits.
			// (twoBitSize/2)*2.5 = (twoBitSize/2)*25/10
			// = twoBitSize*25/(2*10) = twoBitSize*25/20
			// = twoBitSize*5/4
			if size < twoBitSize*5/4 {
				twoBitSize = size
				continue
			}
			// If the distance is more than 3 bits then we probably
			// have bad input; again we allow for some variability by
			// putting the cut-off point at 3.5 bits.
			// Similarly to above, (twoBitSize/2)*3.5 = twoBitSize*7/4
			if size > twoBitSize*7/4 {
				d.curEdge = i
				return fmt.Errorf("bad input: bit distance too long")
			}

			// We found the end of the lead-in.
			d.curEdge = i
			d.bitSize = twoBitSize / 2
			return nil
		}
	}

	d.curEdge = len(edges)
	return fmt.Errorf("bad input: lead-in ran off end of data")
}

// Detect edges in the input samples, ignoring samples that are within
// the given noise floor. This returns the indexes of the input at which
// edges (aka transitions) were detected.
func EdgeDetect(input []int, noiseFloor int) []int {
	// TODO: change this to a pull method that returns one edge? to let
	// the caller adjust parameters on the fly (e.g. glitch length), and
	// to take less memory (both output and allowing >1 input blocks)?
	var edges []int
	// Find the first sample that is not within the noise floor.
	noise := noiseFloor
	i := 0
	for i < len(input) && input[i] < noise && input[i] > -noise {
		i++
	}
	if i >= len(input) {
		// Input contains only noise, so no edges were found.
		return edges
	}
	// TODO: if i == 0, should that be considered a valid edge?
	edges = append(edges, i)

	// Find any remaining edges.
	prevSide := input[i] < 0
	for i++; i < len(input); i++ {
		v := input[i]
		// Ignore samples that are within the noise floor
		if v < noise && v > -noise {
			// TODO: detect long sequences of only noise (end of block)?
			// (maybe provide an "end-edge" where the noise starts? how
			// does the encoding handle the last bit?)
			continue
		}
		// Ignore samples that are on the same side of zero
		if (v < 0) == prevSide {
			continue
		}
		// We found a new edge
		// TODO: check for and skip glitches here? or leave for later?
		// TODO: place the edge at the sample that crossed 0, even if it
		// ended up within the noise at first, and only later came out?
		prevSide = v < 0
		edges = append(edges, i)
	}

	return edges
}
