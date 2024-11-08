package main

import (
	"fmt"
	"os"
	"time"

	"github.com/alexflint/go-arg"

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
	Input  string `arg:"positional,required" help:"input wav file"`
	Output string `arg:"positional" help:"output wav file [out.wav]"`
	// TODO: remove default value text from above help text, when go-arg
	// is updated to a newer version with the fix for auto-printing it.
}{
	Output: "out.wav",
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

	il, ih := samples[0], samples[0]
	ol, oh := il, ih
	func() {
		defer log.Time(1, "Calculating slope...")(" done in")

		prev := 0
		for i := 0; i < len(samples); i++ {
			s := samples[i]

			if s < il {
				il = s
			}
			if s > ih {
				ih = s
			}

			prev, s = s, s-prev
			samples[i] = s

			if s < ol {
				ol = s
			}
			if s > oh {
				oh = s
			}
		}
	}()

	szl, szh := len(fmt.Sprint(il)), len(fmt.Sprint(ih))
	if l := len(fmt.Sprint(ol)); l > szl {
		szl = l
	}
	if h := len(fmt.Sprint(oh)); h > szh {
		szh = h
	}

	fmt.Printf("Sample min: %*d, max: %*d\n", szl, il, szh, ih)
	fmt.Printf("Slope  min: %*d, max: %*d\n", szl, ol, szh, oh)

	err = wav.SaveMono(args.Output, rate, bits, samples)
	if err != nil {
		return err
	}

	return nil
}
