package main

import (
	"fmt"
	"io"
	"math"
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
	Input string `arg:"positional,required" help:"input wave file"`
	Edges string `help:"output edges to this file" placeholder:"FILE"`
	Stats string `help:"output some statistics" placeholder:"FILE"`

	NoiseFloor      int `help:"noise floor; -1 means use 2% of max"`
	MaxCrossingTime int `help:"max samples for 0-crossing before None"`

	NoClean bool `help:"do not clean the input signal first"`
}{
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
	log.F(
		1, "Input: %v %v-bit samples at %v Hz = %v\n",
		len(samples), bits, rate, d(len(samples))*time.Second/d(rate),
	)

	if !args.NoClean {
		if err := cleanSamples(samples, rate, bits); err != nil {
			return err
		}
	}

	ed := initEdgeDetector(samples, rate, bits)

	stats, err := runEdges(ed, args.Stats != "")
	if err != nil {
		return err
	}

	if args.Stats != "" {
		if err := outputStats(stats, args.Stats); err != nil {
			return err
		}
	}

	return nil
}

func openOutput(fn string, retErr *error) (io.Writer, func()) {
	if *retErr != nil || fn == "" {
		return nil, func() {}
	}

	if fn == "-" {
		return os.Stdout, func() {}
	}

	f, err := os.Create(fn)
	if err != nil {
		*retErr = err
		return nil, func() {}
	}
	// TODO: buffering
	return f, func() {
		if err := f.Close(); err != nil && *retErr == nil {
			*retErr = err
		}
	}
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

func initEdgeDetector(samples []int, rate, bits int) *mfm.EdgeDetect {
	ed := mfm.NewEdgeDetect(samples, getNoiseFloor(bits))

	// If a max crossing time was given, use it as-is. Otherwise, we use
	// the expected bit width as the max crossing time, which matches
	// what the DC offset filter does.
	if args.MaxCrossingTime < 0 {
		bitWidth := mfm.ExpectedBitWidth(mfm.DefaultBitRate, rate)
		ed.MaxCrossingTime = int(bitWidth + 0.5)
	} else {
		ed.MaxCrossingTime = args.MaxCrossingTime
	}

	log.F(
		1, "Noise floor: %v, max crossing time: %v\n",
		ed.NoiseFloor, ed.MaxCrossingTime,
	)

	return ed
}

func runEdges(ed *mfm.EdgeDetect, doStats bool) (s *Stats, e error) {
	defer log.Time(1, "Processing edges...\n")("Processing done in")

	var stats *Stats
	if doStats {
		stats = newStats()
	}

	outEdges, closeEdges := openOutput(args.Edges, &e)
	defer closeEdges()

	var esz, ssz, csz int
	if outEdges != nil {
		// Header line:
		// Edge Type Sample 0-crossing Size Duration
		const (
			hdrEdge      = "Edge"
			hdrSample    = "Sample"
			hdrZeroCross = "0-crossing"
			hdrSize      = "Size"
			hdrDuration  = "Duration"
		)

		esz = max(len(fmt.Sprint((len(ed.Samples)+1)/2)), len(hdrEdge))
		ssz = len(fmt.Sprint(len(ed.Samples)))
		csz = max(max(ssz+4, len(hdrZeroCross)), len(hdrDuration))
		ssz = max(max(ssz, len(hdrSample)), len(hdrSize))

		_, err := fmt.Fprintf(
			outEdges, "%*v Type %*v %*v %*v %*v\n",
			esz, hdrEdge, ssz, hdrSample, csz, hdrZeroCross,
			ssz, hdrSize, csz, hdrDuration,
		)
		if err != nil {
			return nil, err
		}
	}

	edges := 0
	for ed.Next() {
		edges++

		if outEdges != nil {
			_, err := fmt.Fprintf(
				outEdges, "%*v  %v-%v %*v %*.3f %*v %*.3f\n",
				esz, edges, ed.PrevType, ed.CurType, ssz, ed.CurIndex,
				csz, ed.CurZero, ssz, ed.CurIndex-ed.PrevIndex,
				csz, ed.CurZero-ed.PrevZero,
			)
			if err != nil {
				return nil, err
			}
		}

		if doStats {
			if err := stats.AddEdge(ed); err != nil {
				return nil, err
			}
		}
	}

	if outEdges != nil {
		_, err := fmt.Fprintf(
			outEdges, "%*v  %v-%v %*v %*.3f %*v %*.3f\n",
			esz, "End", ed.PrevType, ed.CurType, ssz, ed.CurIndex,
			csz, ed.CurZero, ssz, ed.CurIndex-ed.PrevIndex,
			csz, ed.CurZero-ed.PrevZero,
		)
		if err != nil {
			return nil, err
		}
	}

	log.Ln(1, "Edges found:", edges)

	return stats, nil
}

func outputStats(stats *Stats, fn string) (retErr error) {
	outStats, closeStats := openOutput(fn, &retErr)
	defer closeStats()
	if retErr != nil {
		return retErr
	}

	if err := stats.Output(outStats); err != nil {
		return err
	}

	return nil
}

type StatsGroup struct {
	Count, High, Low, None int
	Min, Max, Mean, VarK   float64
}

func (g StatsGroup) Variance() float64 {
	if g.Count < 2 {
		return 0
	}
	return g.VarK / float64(g.Count-1)
}

func (g StatsGroup) StDev() float64 {
	return math.Sqrt(g.Variance())
}

type Stats struct {
	durations map[int]StatsGroup
}

func newStats() *Stats {
	return &Stats{
		durations: map[int]StatsGroup{},
	}
}

func (s *Stats) AddEdge(ed *mfm.EdgeDetect) error {
	val := ed.CurZero - ed.PrevZero

	bucket := int(val)
	g := s.durations[bucket]

	g.Count++

	switch ed.PrevType {
	case mfm.EdgeToHigh:
		g.High++
	case mfm.EdgeToLow:
		g.Low++
	case mfm.EdgeToNone:
		g.None++
	default:
		return fmt.Errorf("unknown edge type: %#v", ed.PrevType)
	}

	// This uses Knuth's method for calculating mean and variance, as
	// shown here: https://math.stackexchange.com/a/116344
	if g.Count == 1 {
		g.Min, g.Max, g.Mean, g.VarK = val, val, val, 0
	} else {
		if val < g.Min {
			g.Min = val
		}
		if val > g.Max {
			g.Max = val
		}

		// m_k = m_k-1 + (x_k - m_k-1) / k
		prevMean := g.Mean
		g.Mean += (val - prevMean) / float64(g.Count)

		// v_k = v_k-1 + (x_k - m_k-1) * (x_k - m_k)
		// I'm not factoring out x_k here because I don't know enough
		// math to say what that would do to the numerical stability.
		g.VarK += (val - prevMean) * (val - g.Mean)
	}

	s.durations[bucket] = g
	return nil
}

func (s *Stats) Output(out io.Writer) error {
	durations := s.durations

	keys := make([]int, 0, len(durations))
	maxCount, maxVar := 0, 0.0
	for k, v := range durations {
		keys = append(keys, k)
		if v.Count > maxCount {
			maxCount = v.Count
		}
		if va := v.Variance(); va > maxVar {
			maxVar = va
		}
	}
	sort.Ints(keys)

	// This is safe because there's always at least one duration count.
	klen := len(fmt.Sprintf("%v", keys[len(keys)-1]))
	ksz := max(klen, len("Group"))
	csz := max(len(fmt.Sprintf("%v", maxCount)), len("Total"))
	msz := max(klen+1+3, len("Mean"))
	vsz := max(len(fmt.Sprintf("%v", int(maxVar)))+1+3, len("Variance"))
	_, err := fmt.Fprintf(
		out, "%*s %*s %*s %*s %*s %*s %*s %*s %*s %*s\n",
		ksz, "Group", csz, "High", csz, "Low", csz, "None",
		csz, "Total", msz, "Min", msz, "Max", msz, "Mean",
		vsz, "StDev", vsz, "Variance",
	)
	if err != nil {
		return err
	}

	var countHigh, countLow, countNone int

	series := false
	for i, k := range keys {
		// This switch puts blank lines where a group of consecutive
		// numbers skips one or more numbers, and between those groups
		// and groups of non-consecutive numbers.
		switch {
		case i == 0:
			// Do nothing
		case k == 1+keys[i-1]:
			series = true
		case series:
			series = false
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		case i+1 < len(keys) && keys[i+1] == 1+k:
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}

		g := durations[k]

		if g.High > 0 {
			countHigh++
		}
		if g.Low > 0 {
			countLow++
		}
		if g.None > 0 {
			countNone++
		}

		_, err := fmt.Fprintf(
			out, "%*v %*v %*v %*v %*v %*.3f %*.3f %*.3f %*.3f %*.3f\n",
			ksz, k, csz, g.High, csz, g.Low, csz, g.None,
			csz, g.Count, msz, g.Min, msz, g.Max, msz, g.Mean,
			vsz, g.StDev(), vsz, g.Variance(),
		)
		if err != nil {
			return err
		}
	}

	wsz := max(len(fmt.Sprintf("%v", len(durations))), len("Total"))
	_, err = fmt.Fprintf(
		out, "Distinct widths:\n%*s %*s %*s %*s\n%*v %*v %*v %*v\n",
		wsz, "High", wsz, "Low", wsz, "None", wsz, "Total",
		wsz, countHigh, wsz, countLow,
		wsz, countNone, wsz, len(durations),
	)
	if err != nil {
		return err
	}

	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
