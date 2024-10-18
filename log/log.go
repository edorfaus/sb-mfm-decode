package log

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Level is the current logging level - the maximum level of logs that
// will actually be output.
var Level int = 1

// Target is where the logging will be output to.
var Target io.Writer = os.Stdout

func Log(level int, v ...any) {
	if Level >= level {
		fmt.Fprint(Target, v...)
	}
}

func Ln(level int, v ...any) {
	if Level >= level {
		fmt.Fprintln(Target, v...)
	}
}

func F(level int, f string, v ...any) {
	if Level >= level {
		fmt.Fprintf(Target, f, v...)
	}
}

func Warn(v ...any) {
	if Level >= 0 {
		fmt.Fprintln(
			Target, append(append([]any(nil), "Warning:"), v...)...,
		)
	}
}

func Time(level int, f string, v ...any) func(...any) {
	if Level < level {
		return func(...any) {}
	}
	fmt.Fprintf(Target, f, v...)
	start := time.Now()
	return func(v ...any) {
		dur := time.Since(start)
		fmt.Fprintln(Target, append(v, dur)...)
	}
}
