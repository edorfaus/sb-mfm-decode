package main

import (
	"bufio"
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
	Input  string `arg:"positional,required" help:"input wav file"`
	Output string `arg:"positional" help:"output text file [out.txt]"`
	// TODO: remove default value text from above help text, when go-arg
	// is updated to a newer version with the fix for auto-printing it.

	LogLevel int  `help:"set the logging level (verbosity)"`
	NoClean  bool `help:"do not clean the input signal first"`

	NoiseFloor int `help:"noise floor; -1 means use 2% of max"`

	BitWidth float64 `help:"base bit width; 0=by sample rate, -1=none"`
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

	if err := runStats(samples, rate, bits, out); err != nil {
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

func runStats(samples []int, rate, bits int, out *bufio.Writer) error {
	defer log.Time(1, "Processing pulses...\n")("Processing done in")

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

	var bwStats Stats
	if pc.BitWidth != 0 {
		bwStats.Add(pc.BitWidth)
	}

	pulseStats := map[[2]mfm.PulseClass][2]Stats{}

	var overall Stats

	prevClass := mfm.PulseUnknown
	prevWidth := 0.0

	for pc.Next() {
		bwStats.Add(pc.BitWidth)

		key := [2]mfm.PulseClass{prevClass, pc.Class}
		s := pulseStats[key]
		s[0].Add(prevWidth)
		s[1].Add(pc.Width)
		pulseStats[key] = s

		overall.Add(pc.Width)

		prevClass, prevWidth = pc.Class, pc.Width
	}

	keys := make([][2]mfm.PulseClass, 0, len(pulseStats))
	maxCount := 0
	for k, v := range pulseStats {
		keys = append(keys, k)
		// v[0].Count == v[1].Count unless something is very wrong.
		if c := v[0].Count; c > maxCount {
			maxCount = c
		}
	}

	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		return a[1] < b[1]
	})

	csz := len(fmt.Sprint(maxCount))
	if sz := len("count"); sz > csz {
		csz = sz
	}
	vsz := len(fmt.Sprintf("%.3f", overall.Max))

	fmt.Fprintf(
		out,
		" - : %*v ; %*v,%-*v - %*v,%-*v ; %*v,%-*v\n",
		csz, "count", vsz, "minA", vsz, "minB", vsz, "maxA",
		vsz, "maxB", vsz, "avgA", vsz, "avgB",
	)

	for _, k := range keys {
		v := pulseStats[k]
		fmt.Fprintf(
			out,
			"%v-%v: %*v ; %*.3f,%*.3f - %*.3f,%*.3f ; %*.3f,%*.3f\n",
			k[0], k[1], csz, v[0].Count,
			vsz, v[0].Min, vsz, v[1].Min,
			vsz, v[0].Max, vsz, v[1].Max,
			vsz, v[0].Avg(), vsz, v[1].Avg(),
		)
	}

	fmt.Fprintf(
		out, "\noverall: %v ; %.3f - %.3f ; %.3f\n",
		overall.Count, overall.Min, overall.Max, overall.Avg(),
	)

	fmt.Fprintf(
		out, "\nbit width: %v ; %.3f - %.3f ; %.3f\n",
		bwStats.Count, bwStats.Min, bwStats.Max, bwStats.Avg(),
	)

	if err := out.Flush(); err != nil {
		return err
	}

	return nil
}

type Stats struct {
	Min, Max, Tot float64

	Count int
}

func (s *Stats) Add(v float64) {
	if s.Count == 0 {
		s.Min, s.Max, s.Tot, s.Count = v, v, v, 1
		return
	}
	if v < s.Min {
		s.Min = v
	}
	if v > s.Max {
		s.Max = v
	}
	s.Tot += v
	s.Count++
}

func (s *Stats) Avg() float64 {
	return s.Tot / float64(s.Count)
}
