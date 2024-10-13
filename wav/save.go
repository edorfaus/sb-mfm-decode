package wav

import (
	"os"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"github.com/edorfaus/sb-mfm-decode/log"
)

func SaveMono(fn string, samples []int, rate, bits int) (er error) {
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
