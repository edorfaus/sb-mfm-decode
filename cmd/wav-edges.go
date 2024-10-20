package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/alexflint/go-arg"

	"github.com/edorfaus/sb-mfm-decode/filter"
	"github.com/edorfaus/sb-mfm-decode/log"
	"github.com/edorfaus/sb-mfm-decode/mfm"
	"github.com/edorfaus/sb-mfm-decode/wav"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

var args = struct {
	Stats  bool   `help:"print some statistics"`
	Input  string `arg:"positional,required" help:"input wav file"`
	Output string `arg:"positional" help:"output wav file [out.wav]"`
	// TODO: remove default value text from above help text, when go-arg
	// is updated to a newer version with the fix for auto-printing it.

	NoiseFloor      int `help:"noise floor; -1 means use 2% of max"`
	MaxCrossingTime int `help:"max samples for 0-crossing before None"`

	NoClean bool `help:"do not clean the input signal first"`
}{
	Output: "out.wav",

	NoiseFloor:      -1,
	MaxCrossingTime: -1,
}

func run() error {
	arg.MustParse(&args)

	samples, meta, err := wav.LoadDataChannel(args.Input)
	if err != nil {
		return err
	}
	rate, bits := meta.SampleRate, meta.BitDepth

	type d = time.Duration
	fmt.Printf(
		"Input: %v %v-bit samples at %v Hz = %v\n",
		len(samples), bits, rate, d(len(samples))*time.Second/d(rate),
	)

	if !args.NoClean {
		if err := cleanSamples(samples, rate, bits); err != nil {
			return err
		}
	}

	if args.Stats {
		h, l := samples[0], samples[0]
		for _, v := range samples {
			h = max(h, v)
			if v < l {
				l = v
			}
		}
		fmt.Printf("Min sample: %v, max: %v\n", l, h)
	}

	start := time.Now()
	output, err := processSamples(samples, rate, bits)
	fmt.Println("Processing done in", time.Since(start))
	if err != nil {
		return err
	}

	err = wav.SaveMono(args.Output, rate, bits, output)
	if err != nil {
		return err
	}

	return nil
}

func getNoiseFloor(bits int) int {
	if args.NoiseFloor >= 0 {
		return args.NoiseFloor
	}
	return filter.DefaultNoiseFloor(bits)
}

func cleanSamples(samples []int, rate, bits int) error {
	defer log.Time(1, "Cleaning waveform...\n")("Cleaning done in")

	noiseFloor := getNoiseFloor(bits)
	peakWidth := filter.MfmPeakWidth(mfm.DefaultBitRate, rate)

	log.Ln(1, "  noise floor:", noiseFloor, "; peak width:", peakWidth)

	f := filter.NewDCOffset(noiseFloor, peakWidth)
	return f.Run(samples, samples)
}

func processSamples(samples []int, rate, bits int) ([]int, error) {
	noiseFloor := getNoiseFloor(bits)

	ed := mfm.NewEdgeDetect(samples, noiseFloor)

	// If a max crossing time was given, use it as-is. Otherwise, we
	// use an MFM decoder temporarily, purely to get the same value as
	// it would initialize MaxCrossingTime to for a given sampling rate.
	// TODO: improve this, maybe make a non-method func for it?
	if args.MaxCrossingTime < 0 {
		bitWidth := mfm.ExpectedBitWidth(mfm.DefaultBitRate, rate)
		mfm.NewDecoder(ed).SetBitWidth(bitWidth)
	} else {
		ed.MaxCrossingTime = args.MaxCrossingTime
	}

	fmt.Printf(
		"Noise floor: %v, max crossing time: %v\n",
		ed.NoiseFloor, ed.MaxCrossingTime,
	)

	// The output will have the same size as the input.
	output := make([]int, len(samples))

	// For simplicity, put the high and low values at 1/2 max amplitude.
	high := 1 << (bits - 2)

	// For statistics
	durCountAll := map[int]int{}
	durCountHigh := map[int]int{}
	durCountLow := map[int]int{}
	durCountNone := map[int]int{}

	fillFrom := 0
	fill := func(edge mfm.EdgeType, from, to int) error {
		duration := to - from
		durCountAll[duration]++

		val := 0
		switch edge {
		case mfm.EdgeToHigh:
			val = high
			durCountHigh[duration]++
		case mfm.EdgeToLow:
			val = -high
			durCountLow[duration]++
		case mfm.EdgeToNone:
			val = 0
			durCountNone[duration]++
		default:
			return fmt.Errorf("unknown edge type: %v", edge)
		}

		if from != fillFrom {
			return fmt.Errorf(
				"fill did not resume at same point: got %v, want %v",
				from, fillFrom,
			)
		}
		fillFrom = to

		for i := from; i < to; i++ {
			output[i] = val
		}

		return nil
	}

	edges := 0
	for ed.Next() {
		edges++

		err := fill(ed.PrevType, ed.PrevIndex, ed.CurIndex)
		if err != nil {
			return nil, err
		}
	}

	if err := fill(ed.PrevType, ed.PrevIndex, ed.CurIndex); err != nil {
		return nil, err
	}

	if fillFrom != len(output) {
		return nil, fmt.Errorf(
			"only filled %v of %v samples", fillFrom, len(output),
		)
	}

	fmt.Println("Edges found:", edges)

	if !args.Stats {
		return output, nil
	}

	// Print some statistics
	keys := make([]int, 0, len(durCountAll))
	maxCount := 0
	for k, v := range durCountAll {
		keys = append(keys, k)
		if v > maxCount {
			maxCount = v
		}
	}
	sort.Ints(keys)
	// This is safe because there's always at least one duration count.
	ksz := max(5, len(fmt.Sprintf("%v", keys[len(keys)-1])))
	vsz := max(5, len(fmt.Sprintf("%v", maxCount)))
	fmt.Printf(
		"%*s %*s %*s %*s %*s\n",
		ksz, "Width", vsz, "High", vsz, "Low", vsz, "None", vsz, "Total",
	)
	for _, k := range keys {
		fmt.Printf(
			"%*v %*v %*v %*v %*v\n",
			ksz, k, vsz, durCountHigh[k], vsz, durCountLow[k],
			vsz, durCountNone[k], vsz, durCountAll[k],
		)
	}

	wsz := max(5, len(fmt.Sprintf("%v", len(durCountAll))))
	fmt.Printf(
		"Distinct widths:\n%*s %*s %*s %*s\n%*v %*v %*v %*v\n",
		wsz, "High", wsz, "Low", wsz, "None", wsz, "Total",
		wsz, len(durCountHigh), wsz, len(durCountLow),
		wsz, len(durCountNone), wsz, len(durCountAll),
	)

	return output, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
