package wav

import (
	"bytes"
	"fmt"
	"os"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"github.com/edorfaus/sb-mfm-decode/log"
)

type Meta struct {
	SampleRate  int
	BitDepth    int
	NumChannels int
}

func readFile(filename string) ([]byte, error) {
	defer log.Time(1, "Reading: %v ...", filename)(" done in")
	return os.ReadFile(filename)
}

// LoadDataChannel loads the wave samples for the data channel from the
// given file.
func LoadDataChannel(filename string) ([]int, Meta, error) {
	data, meta, err := LoadInterleaved(filename)
	if err != nil || meta.NumChannels <= 1 {
		// If NumChannels < 1, then LoadInterleaved gives err != nil.
		return data, meta, err
	}

	// Multiple channels, keep the second (right channel, if stereo).

	defer log.Time(1, "Extracting data channel...")(" done in")

	// Make a new buffer so we can release the oversized one.
	out := make([]int, len(data)/meta.NumChannels)

	for i, j := 0, 1; i < len(out); i, j = i+1, j+meta.NumChannels {
		out[i] = data[j]
	}

	meta.NumChannels = 1

	return out, meta, nil
}

// LoadInterleaved loads the wave samples from the given file, without
// de-interleaving them if there's more than one channel.
func LoadInterleaved(filename string) ([]int, Meta, error) {
	fileData, err := readFile(filename)
	if err != nil {
		return nil, Meta{}, err
	}

	defer log.Time(1, "Decoding WAVE data...\n")("Decoding done in")

	d := wav.NewDecoder(bytes.NewReader(fileData))

	if err := d.FwdToPCM(); err != nil {
		return nil, Meta{}, err
	}

	if d.BitDepth < 8 || d.BitDepth > 64 || d.BitDepth%8 != 0 {
		return nil, Meta{}, fmt.Errorf("bad bit depth: %v", d.BitDepth)
	}
	expectedSamples := int(d.PCMLen() / int64(d.BitDepth/8))
	log.Ln(2, "Expected samples:", expectedSamples)

	// +1 just in case our calculation isn't quite right.
	buf := &audio.IntBuffer{
		Data: make([]int, expectedSamples+1),
	}
	n, err := d.PCMBuffer(buf)
	if err != nil {
		return nil, Meta{}, err
	}
	buf.Data = buf.Data[:n]
	log.Ln(2, "     Got samples:", n)

	if n > expectedSamples {
		log.Warn("unexpected sample, may have lost some")
	}
	if n < expectedSamples {
		log.Warn("got fewer samples than expected")
	}

	if err := d.Err(); err != nil {
		return nil, Meta{}, err
	}

	if buf.Format == nil || buf.Format.NumChannels < 1 {
		err := fmt.Errorf("missing or bad PCM format information")
		return nil, Meta{}, err
	}

	meta := Meta{
		SampleRate:  buf.Format.SampleRate,
		BitDepth:    buf.SourceBitDepth,
		NumChannels: buf.Format.NumChannels,
	}
	return buf.Data, meta, nil
}
