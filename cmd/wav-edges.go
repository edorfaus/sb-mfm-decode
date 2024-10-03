package main

import (
	"bytes"
	"fmt"
	"os"
	"time"

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

func args() (in, out string, err error) {
	if len(os.Args) > 1 {
		in = os.Args[1]
	} else {
		fmt.Fprintln(os.Stderr, "Usage: wav-edges <in.wav> [out.wav]")
		err = fmt.Errorf("missing input filename argument")
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	} else {
		out = "out.wav"
	}
	return
}

func run() error {
	inf, outf, err := args()
	if err != nil {
		return err
	}

	samples, rate, bits, err := loadSamples(inf)
	if err != nil {
		return err
	}

	type d = time.Duration
	fmt.Printf(
		"Input: %v %v-bit samples at %v Hz: %v\n",
		len(samples), bits, rate, d(len(samples))*time.Second/d(rate),
	)

	output, err := processSamples(samples, rate, bits)
	if err != nil {
		return err
	}

	if err := saveSamples(outf, output, rate, bits); err != nil {
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

	fillFrom := 0
	fill := func(edge mfm.EdgeType, from, to int) error {
		val := 0
		switch edge {
		case mfm.EdgeToHigh:
			val = high
		case mfm.EdgeToLow:
			val = -high
		case mfm.EdgeToNone:
			val = 0
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

	return output, nil
}

func loadSamples(filename string) (_ []int, rate, bits int, _ error) {
	fileData, err := os.ReadFile(filename)
	if err != nil {
		return nil, 0, 0, err
	}

	d := wav.NewDecoder(bytes.NewReader(fileData))

	// TODO: replace FullPCMBuffer with a better way to load the data.
	buf, err := d.FullPCMBuffer()
	if err != nil {
		return nil, 0, 0, err
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

		// Make a new buffer so we can release the oversized one.
		data = make([]int, buf.NumFrames())

		channels := buf.Format.NumChannels
		for i, j := 0, 1; i < len(data); i, j = i+1, j+channels {
			data[i] = buf.Data[j]
		}
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
