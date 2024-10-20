package main

import (
	"bufio"
	"fmt"
	"os"
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
	Input  string `arg:"positional,required" help:"input wav file"`
	Output string `arg:"positional" help:"output text file [out.txt]"`
	// TODO: remove default value text from above help text, when go-arg
	// is updated to a newer version with the fix for auto-printing it.

	LogLevel int  `help:"set the logging level (verbosity)"`
	NoClean  bool `help:"do not clean the input signal first"`

	NoiseFloor int `help:"noise floor; -1 means use 2% of max"`

	BitWidth float64 `help:"base bit width; 0=by sample rate, -1=none"`

	All bool `help:"output detail info about all pulses"`
}{
	Output:     "out.txt",
	LogLevel:   log.Level,
	NoiseFloor: -1,
}

func run() (retErr error) {
	argParser := arg.MustParse(&args)
	if args.BitWidth < 2 && args.BitWidth != 0 && args.BitWidth != -1 {
		argParser.Fail("bit width must be 0, -1, or at least 2")
	}

	log.Level = args.LogLevel

	samples, meta, err := wav.LoadDataChannel(args.Input)
	if err != nil {
		return err
	}
	rate, bits := meta.SampleRate, meta.BitDepth

	type d = time.Duration
	log.F(
		1, "Input: %v %v-bit samples at %v Hz = %v\n",
		len(samples), bits, rate, d(len(samples))*time.Second/d(rate),
	)

	if !args.NoClean {
		if err := cleanSamples(samples, rate, bits); err != nil {
			return err
		}
	}

	var out *bufio.Writer
	if args.Output == "-" {
		out = bufio.NewWriter(os.Stdout)
	} else {
		f, err := os.Create(args.Output)
		if err != nil {
			return err
		}
		defer func() {
			if err := f.Close(); err != nil && retErr == nil {
				retErr = err
			}
		}()
		out = bufio.NewWriter(f)
	}
	defer func() {
		if err := out.Flush(); err != nil && retErr == nil {
			retErr = err
		}
	}()

	if err := classify(samples, rate, bits, out); err != nil {
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
	var peakWidth int
	if args.BitWidth > 0 {
		peakWidth = int(args.BitWidth + 0.5)
	} else {
		peakWidth = filter.MfmPeakWidth(mfm.DefaultBitRate, rate)
	}

	log.Ln(2, "  noise floor:", noiseFloor, "; peak width:", peakWidth)

	f := filter.NewDCOffset(noiseFloor, peakWidth)
	return f.Run(samples, samples)
}

func classify(samples []int, rate, bits int, out *bufio.Writer) error {
	defer log.Time(1, "Classifying pulses...\n")("Classifying done in")

	noiseFloor := getNoiseFloor(bits)
	pc := mfm.NewPulseClassifier(mfm.NewEdgeDetect(samples, noiseFloor))

	switch {
	case args.BitWidth < 0:
		// Do not set the bit width, use the lead-in to find it.
	case args.BitWidth == 0:
		pc.SetBitWidth(mfm.ExpectedBitWidth(mfm.DefaultBitRate, rate))
	default:
		pc.SetBitWidth(args.BitWidth)
	}

	log.F(
		2, "  noise floor: %v, bit width: %v, max crossing time: %v\n",
		pc.Edges.NoiseFloor, pc.BitWidth, pc.Edges.MaxCrossingTime,
	)

	// For statistics
	pulseCounts := map[mfm.PulseClass]int{}

	needNL := false
	if args.All {
		ssz := max(5, len(fmt.Sprint(len(samples))))
		psz := max(5, len(fmt.Sprint(len(samples)/2)))
		fmt.Fprintf(
			out, "%-*s Kind %-*s %-*s %-*s BitWidth\n",
			psz, "Pulse", ssz, "From", ssz, "To", ssz, "Width",
		)

		for i := 0; pc.Next(); i++ {
			pulseCounts[pc.Class]++

			fmt.Fprintf(
				out, "%*v %s:%s%s %*v %*v %*v %8.4f\n",
				psz, i, pc.Class, pc.Edges.PrevType, pc.Edges.CurType,
				ssz, pc.Edges.PrevIndex, ssz, pc.Edges.CurIndex,
				ssz, pc.Width, pc.BitWidth,
			)
		}
	} else {
		for pc.Next() {
			pulseCounts[pc.Class]++

			if pc.Class.Valid() && !pc.TouchesNone() {
				out.WriteString(pc.Class.String())
				needNL = true
			} else {
				if needNL {
					out.WriteByte('\n')
					needNL = false
				}
				fmt.Fprintf(
					out,
					"-- Class:%s Type:%v-%v From:%v To:%v Width:%v"+
						" BitWidth:%v\n",
					pc.Class, pc.Edges.PrevType, pc.Edges.CurType,
					pc.Edges.PrevIndex, pc.Edges.CurIndex,
					pc.Width, pc.BitWidth,
				)
			}
		}
	}
	if needNL {
		out.WriteByte('\n')
		needNL = false
	}
	if err := out.Flush(); err != nil {
		return err
	}

	pulses := 0
	for _, v := range pulseCounts {
		pulses += v
	}
	log.Ln(2, "  pulses found:", pulses, ":", pulseCounts)

	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
