package wav

import (
	"fmt"
	"os"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"github.com/edorfaus/sb-mfm-decode/log"
)

func SaveMono(fn string, rate, bits int, samples []int) (er error) {
	defer log.Time(1, "Saving WAVE to: %v ...", fn)(" done in")

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

func SaveChannels(fn string, rate, bits int, data ...[]int) (e error) {
	numChannels := len(data)
	if numChannels <= 0 {
		return fmt.Errorf("must have at least one channel of samples")
	}
	if numChannels == 1 {
		return SaveMono(fn, rate, bits, data[0])
	}

	defer log.Time(1, "Saving WAVE to: %v ...", fn)(" done in")

	f, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil && e == nil {
			e = err
		}
	}()

	maxSamples := 0
	for _, ch := range data {
		if len(ch) > maxSamples {
			maxSamples = len(ch)
		}
	}

	// Buffer 1M samples at a time (takes 8M RAM, writes 2M at a time),
	// rounded down to the nearest whole number of frames.
	bufFrames := 1024 * 1024 / numChannels
	if bufFrames < 1 {
		// Pathological case: way too many channels; just do one frame.
		bufFrames = 1
	}
	if bufFrames > maxSamples {
		bufFrames = maxSamples
	}
	bufSamples := bufFrames * numChannels

	enc := wav.NewEncoder(f, rate, bits, numChannels, 1)

	buf := &audio.IntBuffer{
		Format: &audio.Format{
			NumChannels: numChannels,
			SampleRate:  rate,
		},
		SourceBitDepth: bits,

		Data: make([]int, 0, bufSamples),
	}

	frame := 0
	for frame < maxSamples {
		b := buf.Data[:0]
		for frame < maxSamples && len(b) < bufSamples {
			for _, ch := range data {
				if frame < len(ch) {
					b = append(b, ch[frame])
				} else {
					b = append(b, 0)
				}
			}
			frame++
		}
		buf.Data = b

		if err := enc.Write(buf); err != nil {
			return err
		}
	}

	if err := enc.Close(); err != nil {
		return err
	}

	return nil
}
