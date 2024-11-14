package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
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
	Output string `arg:"positional" help:"output text file"`

	LogLevel int  `help:"set the logging level (verbosity)"`
	NoClean  bool `help:"do not clean the input signal first"`

	NoiseFloor int `help:"noise floor; -1 means use 2% of max"`

	BitWidth float64 `help:"base bit width; 0=by sample rate, -1=none"`
}{
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
	if args.Output == "" || args.Output == "-" {
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

	// 0:first pulse, 1:second pulse, 2:difference (1-0)
	pulseStats := map[[2]mfm.PulseClass][3]Stats{}

	var overall Stats

	if !pc.Next() {
		return fmt.Errorf("no pulses were found")
	}
	bwStats.Add(pc.BitWidth)
	overall.Add(pc.Width)

	prevClass, prevWidth := pc.Class, pc.Width

	for pc.Next() {
		bwStats.Add(pc.BitWidth)

		key := [2]mfm.PulseClass{prevClass, pc.Class}
		s := pulseStats[key]
		s[0].Add(prevWidth)
		s[1].Add(pc.Width)
		s[2].Add(pc.Width - prevWidth)
		pulseStats[key] = s

		overall.Add(pc.Width)

		prevClass, prevWidth = pc.Class, pc.Width
	}

	// Stats generated, now format and output them.

	keys := make([][2]mfm.PulseClass, 0, len(pulseStats))

	c := NewColumnar(
		out,
		"%*s-%*s: %*d ; %*.3f - %*.3f, %*.3f ; %*.3f - %*.3f, %*.3f"+
			" ; %*.3f - %*.3f, %*.3f\n",
	)

	for k, v := range pulseStats {
		keys = append(keys, k)
		// v[0].Count == v[1].Count unless something is very wrong.
		c.Values(
			"", "", v[0].Count,
			v[0].Min, v[0].Max, v[0].Avg(),
			v[1].Min, v[1].Max, v[1].Avg(),
			v[2].Min, v[2].Max, v[2].Avg(),
		)
	}

	const first = 1
	const second = 1 - first
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a[first] != b[first] {
			return a[first] < b[first]
		}
		return a[second] < b[second]
	})

	c.Headers(
		"a", "b", "count",
		"A: min", "max", "avg",
		"B: min", "max", "avg",
		"B-A: min", "max", "avg",
	)

	for _, k := range keys {
		v := pulseStats[k]
		c.OutValues(
			k[0], k[1], v[0].Count,
			v[0].Min, v[0].Max, v[0].Avg(),
			v[1].Min, v[1].Max, v[1].Avg(),
			v[2].Min, v[2].Max, v[2].Avg(),
		)
	}

	out.WriteByte('\n')

	c = NewColumnar(out, "%*s: %*d ; %*.3f - %*.3f, %*.3f\n")
	v := overall
	c.Values("all pulses", v.Count, v.Min, v.Max, v.Avg())
	v = bwStats
	c.Values("bit widths", v.Count, v.Min, v.Max, v.Avg())

	v = overall
	c.OutValues("all pulses", v.Count, v.Min, v.Max, v.Avg())
	v = bwStats
	c.OutValues("bit widths", v.Count, v.Min, v.Max, v.Avg())

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

type Columnar struct {
	Output *bufio.Writer
	Format []string
	OutFmt []string
	Prefix []string
	Suffix string
	Size   []int
}

func NewColumnar(out *bufio.Writer, formatString string) *Columnar {
	re := regexp.MustCompile("%[^%a-zA-Z]*[%a-zA-Z]")
	m := re.FindAllStringIndex(formatString, -1)

	format := make([]string, 0, len(m))
	outFmt := make([]string, 0, len(format))
	prefix := make([]string, 0, len(format))
	from := 0
	for _, loc := range m {
		if formatString[loc[1]-1] == '%' {
			// This was a %% so keep going
			continue
		}

		fmtStr := formatString[loc[0]:loc[1]]
		format = append(format, strings.ReplaceAll(fmtStr, "*", ""))
		outFmt = append(outFmt, fmtStr)
		prefix = append(prefix, fmt.Sprintf(formatString[from:loc[0]]))

		from = loc[1]
	}
	suffix := fmt.Sprintf(formatString[from:])

	return &Columnar{
		Output: out,
		Format: format,
		OutFmt: outFmt,
		Prefix: prefix,
		Suffix: suffix,
		Size:   make([]int, len(format)),
	}
}

func (c *Columnar) Values(vals ...any) {
	f, sz := c.Format, c.Size
	for i, v := range vals {
		if s := len(fmt.Sprintf(f[i], v)); s > sz[i] {
			sz[i] = s
		}
	}
}

func (c *Columnar) Headers(hdr ...string) {
	out, p, sz := c.Output, c.Prefix, c.Size
	if len(hdr) != len(p) {
		panic(fmt.Errorf(
			"bad header count: is %v, expected %v", len(hdr), len(p),
		))
	}
	for i, v := range hdr {
		if s := len(v); s > sz[i] {
			sz[i] = s
		}
		out.WriteString(p[i])
		fmt.Fprintf(out, "%*s", sz[i], v)
	}
	out.WriteString(c.Suffix)
}

func (c *Columnar) OutValues(vals ...any) {
	out, f, p, sz := c.Output, c.OutFmt, c.Prefix, c.Size
	for i, v := range vals {
		out.WriteString(p[i])
		fmt.Fprintf(out, f[i], sz[i], v)
	}
	out.WriteString(c.Suffix)
}
