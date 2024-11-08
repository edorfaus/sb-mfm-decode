# MFM decoder for StudyBox tapes

This project is intended to take an input WAVE file that contains a
recording of a StudyBox tape, in particular the data track, and decode
that to a binary file containing the actual data that the console would
load from the tape.

It should be robust, in that it should handle most sample rates, poor
quality tapes (within limits), etc., and either produce useful output,
or complain about it so the user knows something is wrong.

_It is currently **incomplete**, a work in progress, but does have some
test programs that, while mostly created to test and debug the library
code, may still be useful depending on your needs._

When done, the project will hopefully provide both a CLI program that
does the entire process, and library code that could be used to add such
functionality to other programs.

## Test programs

Note that any or all of these may be changed, replaced or removed in the
future, as they are not meant to be a final product of this project.

- `cmd/dc-offset.go` : This takes an input WAVE file, runs some cleanup
	on it to remove DC offset and certain forms of noise, and outputs
	the result as a new WAVE file. (It can also output the difference.)
	The cleanup filter this uses is also used by the other programs (at
	least by default) to clean up the input before they do their thing.
- `cmd/wav-edges.go` : This takes an input WAVE file, runs the edge
	detector on it, and outputs a new WAVE file with those edges output
	as a square wave in the same places as the input. Thus, it shows
	where in the input the edge detector sees the edges. It can also
	(optionally) output some statistics on the durations between the
	detected edges, both separated by type (high/low/none) and combined.
- `cmd/zc-edges.go` : This takes an input WAVE file, runs the edge
	detector on it, and using the interpolated zero crossings,
	optionally outputs a listing of the detected edges, and/or some
	statistics on the durations between the edges, to separate files.
- `cmd/classify.go` : This takes an input WAVE file, runs the edge
	detector and the pulse classifier on it, and outputs the results to
	a text file.
- `cmd/mfm-decode.go` : This is the oldest, and currently least useful,
	test program. It does not take input, uses stdout for results, and
	uses some old decoder code that needs significant changes.
- `cmd/wav-slope.go` : This takes an input WAVE file, calculates the
	instantaneous slope of the waveform at each sample, and outputs a
	new WAVE file with the result. Each sample of the output is the
	slope from the previous input sample to that input sample.
