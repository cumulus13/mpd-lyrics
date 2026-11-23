package termx

import (
	"os"
	"strconv"
)

// Size returns the terminal size in cells, falling back to $COLUMNS/$LINES and
// finally 80x24.
func Size() (cols, rows int) {
	if c, r, ok := size(); ok {
		return c, r
	}
	cols, rows = 80, 24
	if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v > 0 {
		cols = v
	}
	if v, err := strconv.Atoi(os.Getenv("LINES")); err == nil && v > 0 {
		rows = v
	}
	return cols, rows
}

// MakeRaw puts stdin into non-canonical, no-echo mode and returns a function
// that restores the previous state.
func MakeRaw() (restore func(), err error) { return makeRaw() }

// EnableVT turns on ANSI escape processing where the OS needs it (Windows).
func EnableVT() error { return enableVT() }

// IsTerminal reports whether f is a character device.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
