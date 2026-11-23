// Package termx contains the small amount of terminal plumbing mpd-lyrics
// needs without third-party dependencies: size, raw mode, key decoding and
// display-width calculation.
package termx

import "unicode"

// RuneWidth returns the number of terminal cells r occupies: 0 for control
// and combining characters, 2 for East Asian wide characters and emoji, else 1.
// The table is intentionally compact; it covers the scripts that appear in
// lyrics (CJK, Hangul, kana, emoji) rather than all of Unicode.
func RuneWidth(r rune) int {
	switch {
	case r == 0, r < 32, r >= 0x7f && r < 0xa0:
		return 0
	case r < 0x300:
		return 1
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r):
		return 0
	case isWide(r):
		return 2
	}
	return 1
}

func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0x303E,
		r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1F64F,
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x20000 && r <= 0x3FFFD:
		return true
	}
	return false
}

// StringWidth is the display width of s in cells.
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

// Truncate cuts s to at most width cells, appending an ellipsis when cut.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if StringWidth(s) <= width {
		return s
	}
	out := make([]rune, 0, len(s))
	w := 0
	for _, r := range s {
		rw := RuneWidth(r)
		if w+rw > width-1 {
			break
		}
		out = append(out, r)
		w += rw
	}
	return string(out) + "…"
}
