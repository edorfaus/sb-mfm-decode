package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/exp/slices"

	"github.com/alexflint/go-arg"

	"github.com/edorfaus/sb-mfm-decode/filter"
	"github.com/edorfaus/sb-mfm-decode/log"
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
	Debug bool `help:"print verbose debug info (log level 4)"`

	NoiseFloor int  `help:"noise floor; -1 means use 2% of max"`
	PeakWidth  int  `help:"width of a peak; 0 means use default"`
	Offsets    bool `help:"output offsets instead of adjusted samples"`
	Stereo     bool `help:"output both offsets and samples as stereo"`
}{
	Output:     "out.wav",
	NoiseFloor: -1,
}

func run() error {
	arg.MustParse(&args)

	if args.Debug {
		log.Level = 4
	}

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

	if args.Stats {
		l, h := slices.Min(samples), slices.Max(samples)
		fmt.Printf("Min sample: %v, max: %v\n", l, h)
	}

	start := time.Now()
	output, err := processSamples(samples, rate, bits)
	fmt.Println("Processing done in", time.Since(start))
	if err != nil {
		return err
	}

	if args.Stereo {
		for i, v := range output {
			samples[i] -= v
		}
		err = wav.SaveChannels(args.Output, rate, bits, output, samples)
	} else {
		err = wav.SaveMono(args.Output, rate, bits, output)
	}
	if err != nil {
		return err
	}

	return nil
}

func processSamples(samples []int, rate, bits int) ([]int, error) {
	noiseFloor := filter.DefaultNoiseFloor(bits)
	if args.NoiseFloor >= 0 {
		noiseFloor = args.NoiseFloor
	}

	peakWidth := filter.MfmPeakWidth(4800, rate)
	if args.PeakWidth > 0 {
		peakWidth = args.PeakWidth
	}

	f := filter.NewDCOffset(noiseFloor, peakWidth)

	fmt.Printf(
		"Noise floor: %v, peak width: %v\n",
		f.NoiseFloor, f.PeakWidth,
	)

	output := f.Run(samples)

	if args.Stats {
		total := 0.0
		for _, v := range output {
			total += float64(v)
		}

		fmt.Printf(
			"Offsets: min: %v, max: %v, avg: %.3v\n",
			slices.Min(output), slices.Max(output),
			total/float64(len(output)),
		)
	}

	// This is tricky: instead of checking if neither is set, which
	// would make it always output samples to the right channel, this
	// makes it output samples to the right (or mono) by default, and to
	// the left if both are given.
	if args.Offsets == args.Stereo {
		for i, v := range output {
			output[i] = samples[i] - v
		}
	}

	return output, nil
}