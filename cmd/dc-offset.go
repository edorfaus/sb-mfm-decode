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
		fmt.Printf("Input sample min: %v, max: %v\n", l, h)
	}

	output, err := runFilter(samples, rate, bits)
	if err != nil {
		return err
	}

	if args.Stats || args.Offsets || args.Stereo {
		func() {
			log.Time(2, "Recalculating offsets...")(" done in")
			for i, v := range output {
				samples[i] -= v
			}
		}()
	}

	if args.Stats {
		outputStats(samples, output)
	}

	if args.Offsets {
		samples, output = output, samples
	}

	if args.Stereo {
		err = wav.SaveChannels(args.Output, rate, bits, samples, output)
	} else {
		err = wav.SaveMono(args.Output, rate, bits, output)
	}
	if err != nil {
		return err
	}

	return nil
}

func runFilter(samples []int, rate, bits int) ([]int, error) {
	output := samples
	if args.Stats || args.Offsets || args.Stereo {
		output = make([]int, len(samples))
	}

	defer log.Time(1, "Running filter...\n")("Filter done in")

	noiseFloor := filter.DefaultNoiseFloor(bits)
	if args.NoiseFloor >= 0 {
		noiseFloor = args.NoiseFloor
	}

	peakWidth := filter.MfmPeakWidth(4800, rate)
	if args.PeakWidth > 0 {
		peakWidth = args.PeakWidth
	}

	log.F(1, "Noise floor: %v, peak width: %v\n", noiseFloor, peakWidth)

	f := filter.NewDCOffset(noiseFloor, peakWidth)
	return output, f.Run(samples, output)
}

func outputStats(samples, output []int) {
	total := 0.0
	var ol, oh, sl, sh int

	func() {
		log.Time(2, "Running stats...")(" done in")
		sl, sh = slices.Min(output), slices.Max(output)
		ol, oh = samples[0], samples[0]
		for _, v := range samples {
			total += float64(v)
			if v < ol {
				ol = v
			}
			if v > oh {
				oh = v
			}
		}
	}()

	fmt.Printf(
		"Offsets: min: %v, max: %v, avg: %.3v\n",
		ol, oh, total/float64(len(output)),
	)
	fmt.Printf("Output sample min: %v, max: %v\n", sl, sh)
}
