package ui

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
)

// Screen writes rows to a terminal, redrawing only rows that changed.
type Screen struct {
	out     io.Writer
	profile colorx.Profile
	caps    Caps

	prev []string
	sig  string
}

// NewScreen creates a Screen.
func NewScreen(out io.Writer, p colorx.Profile, caps Caps) *Screen {
	return &Screen{out: out, profile: p, caps: caps}
}

// Enter switches to the alternate screen and hides the cursor.
func (s *Screen) Enter() error {
	_, err := io.WriteString(s.out, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")
	return err
}

// Leave restores the main screen and cursor.
func (s *Screen) Leave() error {
	_, err := io.WriteString(s.out, "\x1b[0m\x1b[?25h\x1b[?1049l")
	return err
}

// Reset forces a full redraw on the next Draw.
func (s *Screen) Reset() { s.prev, s.sig = nil, "" }

// Draw paints rows (len(rows) = terminal height).
func (s *Screen) Draw(rows []Row) error {
	enc := make([]string, len(rows))
	skip := make([]bool, len(rows))
	var sig strings.Builder
	fmt.Fprintf(&sig, "%d|", len(rows))
	for i, r := range rows {
		sig.WriteByte(byte('0' + r.scale()))
		sig.WriteByte(byte('0' + r.Part))
		enc[i], skip[i] = s.encode(r)
	}

	var b bytes.Buffer
	full := sig.String() != s.sig || len(s.prev) != len(rows)
	if full {
		b.WriteString("\x1b[2J")
	}
	for i := range rows {
		if skip[i] {
			continue
		}
		if !full && enc[i] == s.prev[i] {
			continue
		}
		fmt.Fprintf(&b, "\x1b[%d;1H%s\x1b[0m\x1b[K", i+1, enc[i])
	}
	s.prev, s.sig = enc, sig.String()
	if b.Len() == 0 {
		return nil
	}
	var out bytes.Buffer
	out.WriteString("\x1b[?2026h") // synchronized update, ignored where unsupported
	out.Write(b.Bytes())
	out.WriteString("\x1b[?2026l")
	_, err := s.out.Write(out.Bytes())
	return err
}

// encode renders one row. skip is true for rows that must not be written
// because they belong to a scaled glyph drawn from the row above (kitty).
func (s *Screen) encode(r Row) (string, bool) {
	sc := r.scale()
	if s.caps.Method != "dec" && s.caps.Method != "kitty" {
		sc = 1
	}
	if s.caps.Method == "kitty" && sc > 1 && r.Part > 0 {
		return "", true
	}

	var b strings.Builder
	if s.caps.Method == "dec" {
		switch {
		case sc >= 2 && r.Part == 0:
			b.WriteString("\x1b#3")
		case sc >= 2:
			b.WriteString("\x1b#4")
		default:
			b.WriteString("\x1b#5")
		}
	}
	for _, seg := range r.Segs {
		if seg.Gap > 0 {
			n := seg.Gap
			if s.caps.Method == "dec" && sc > 1 {
				n /= sc
			}
			b.WriteString(strings.Repeat(" ", n))
			continue
		}
		if seg.Text == "" {
			continue
		}
		b.WriteString(s.sgr(seg))
		if s.caps.Method == "kitty" && sc > 1 {
			fmt.Fprintf(&b, "\x1b]66;s=%d;%s\x1b\\", sc, seg.Text)
		} else {
			b.WriteString(seg.Text)
		}
		b.WriteString("\x1b[0m")
	}
	return b.String(), false
}

func (s *Screen) sgr(seg Seg) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if seg.Bold {
		b.WriteString(";1")
	}
	if seg.Dim {
		b.WriteString(";2")
	}
	if seg.HasColor {
		if fg := s.profile.FG(seg.Color); fg != "" {
			b.WriteString(";" + fg)
		}
	}
	b.WriteString("m")
	return b.String()
}
