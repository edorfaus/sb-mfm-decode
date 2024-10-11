package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"github.com/edorfaus/sb-mfm-decode/mfm"
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
}{
	Output: "out.wav",
}

func run() error {
	arg.MustParse(&args)

	samples, rate, bits, err := loadSamples(args.Input)
	if err != nil {
		return err
	}

	type d = time.Duration
	fmt.Printf(
		"Input: %v %v-bit samples at %v Hz = %v\n",
		len(samples), bits, rate, d(len(samples))*time.Second/d(rate),
	)

	start := time.Now()
	output, err := processSamples(samples, rate, bits)
	fmt.Println("Processing done in", time.Since(start))
	if err != nil {
		return err
	}

	start = time.Now()
	fmt.Printf("Writing: %v ...", args.Output)
	err = saveSamples(args.Output, output, rate, bits)
	fmt.Println(" done in", time.Since(start))
	if err != nil {
		return err
	}

	return nil
}

func processSamples(samples []int, rate, bits int) ([]int, error) {
	// For now, we set the noise floor at 2% of the max value.
	ed := mfm.NewEdgeDetect(samples, (1<<(bits-1))*2/100)

	// Use an MFM decoder temporarily, purely to get the same value as
	// it would initialize MaxCrossingTime to for a given sampling rate.
	// TODO: improve this, maybe make a non-method func for it?
	mfm.NewDecoder(ed).InitFreq(mfm.DefaultBitRate, rate)

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

func loadSamples(filename string) (_ []int, rate, bits int, _ error) {
	fmt.Print("Reading: ", filename, " ...")
	start := time.Now()
	fileData, err := os.ReadFile(filename)
	fmt.Println(" done in", time.Since(start))
	if err != nil {
		return nil, 0, 0, err
	}

	fmt.Println("Decoding...")
	start = time.Now()
	d := wav.NewDecoder(bytes.NewReader(fileData))

	if err := d.FwdToPCM(); err != nil {
		return nil, 0, 0, err
	}

	if d.BitDepth < 8 || d.BitDepth > 64 || d.BitDepth%8 != 0 {
		return nil, 0, 0, fmt.Errorf("bad bit depth: %v", d.BitDepth)
	}
	expectedSamples := int(d.PCMLen() / int64(d.BitDepth/8))
	//fmt.Println("Expected samples:", expectedSamples)

	// +1 just in case our calculation isn't quite right.
	buf := &audio.IntBuffer{
		Data: make([]int, expectedSamples+1),
	}
	n, err := d.PCMBuffer(buf)
	if err != nil {
		return nil, 0, 0, err
	}
	buf.Data = buf.Data[:n]
	//fmt.Println("     Got samples:", n)
	fmt.Println("Decoding done in", time.Since(start))
	if n > expectedSamples {
		fmt.Println("Warning: unexpected sample, may have lost some")
	}
	if n < expectedSamples {
		fmt.Println("Warning: got fewer samples than expected")
	}

	if err := d.Err(); err != nil {
		return nil, 0, 0, err
	}

	if buf.Format == nil || buf.Format.NumChannels < 1 {
		err := fmt.Errorf("missing or bad PCM format information")
		return nil, 0, 0, err
	}

	var data []int
	if buf.Format.NumChannels == 1 {
		data = buf.Data
	} else {
		// Multiple channels, keep the second one (right, if stereo).
		fmt.Print("Extracting second channel...")
		start := time.Now()

		// Make a new buffer so we can release the oversized one.
		data = make([]int, buf.NumFrames())

		channels := buf.Format.NumChannels
		for i, j := 0, 1; i < len(data); i, j = i+1, j+channels {
			data[i] = buf.Data[j]
		}
		fmt.Println(" done in", time.Since(start))
	}

	return data, buf.Format.SampleRate, buf.SourceBitDepth, nil
}

func saveSamples(fn string, samples []int, rate, bits int) (er error) {
	f, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil && er == nil {
			er = err
		}
	}()

	e := wav.NewEncoder(f, rate, bits, 1, 1)

	buf := &audio.IntBuffer{
		Format: &audio.Format{
			NumChannels: 1,
			SampleRate:  rate,
		},

		Data: samples,

		SourceBitDepth: bits,
	}
	if err := e.Write(buf); err != nil {
		return err
	}

	if err := e.Close(); err != nil {
		return err
	}

	return nil
}
