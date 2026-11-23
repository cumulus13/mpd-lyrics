package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
	"github.com/cumulus13/mpd-lyrics/internal/lrc"
	"github.com/cumulus13/mpd-lyrics/internal/termx"
)

const helpText = "q quit · m mode · +/- offset · [ ] size · d direction · space pause · n/p track · r reload · ? help"

// textSeg builds a styled segment, honouring Mono mode.
func (f *Frame) textSeg(text string, c colorx.RGB, bold, current bool) Seg {
	s := Seg{Text: text, Color: c, HasColor: true, Bold: bold}
	if f.Mono {
		s.Bold = current
		s.Dim = !current
	}
	return s
}

// layoutChrome fills the header and footer rows and returns the body range
// [top, bottom).
func layoutChrome(f *Frame, rows []Row) (top, bottom int) {
	top, bottom = 0, f.H
	if f.UI.ShowTitle && f.H >= 5 {
		rows[0] = headerRow(f)
		top = 1
	}
	if f.UI.ShowFooter && f.H >= 6 {
		rows[f.H-1] = footerRow(f)
		bottom = f.H - 1
	}
	return top, bottom
}

func headerRow(f *Frame) Row {
	left := "♪ "
	switch {
	case f.Artist != "" && f.Title != "":
		left += f.Artist + " — " + f.Title
	case f.Title != "":
		left += f.Title
	default:
		left += "mpd-lyrics"
	}

	var parts []string
	if f.Playing {
		parts = append(parts, "▶")
	} else {
		parts = append(parts, "⏸")
	}
	if f.Lyrics != nil && !f.Lyrics.Empty() {
		switch {
		case f.Lyrics.Estimated:
			parts = append(parts, "≈ estimated")
		case f.Source != "":
			parts = append(parts, f.Source)
		}
	}
	if f.OffsetMS != 0 {
		parts = append(parts, fmt.Sprintf("%+dms", f.OffsetMS))
	}
	right := strings.Join(parts, "  ")

	rw := termx.StringWidth(right)
	left = termx.Truncate(left, f.W-rw-2)
	gap := f.W - termx.StringWidth(left) - rw
	if gap < 1 {
		gap = 1
	}
	d := f.Theme.Dim()
	return Row{Segs: []Seg{
		f.textSeg(left, d, false, false),
		{Gap: gap},
		f.textSeg(right, d, false, false),
	}}
}

func footerRow(f *Frame) Row {
	switch {
	case f.Toast != "":
		return centered(f, termx.Truncate(f.Toast, f.W), f.Theme.Current, true, true)
	case f.UI.ShowHelp:
		return centered(f, termx.Truncate(helpText, f.W), f.Theme.Dim(), false, false)
	}
	return Row{}
}

// centered builds a scale-1 row with s centred.
func centered(f *Frame, s string, c colorx.RGB, bold, current bool) Row {
	pad := (f.W - termx.StringWidth(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return Row{Segs: []Seg{{Gap: pad}, f.textSeg(s, c, bold, current)}}
}

// messageBody shows a status line in the middle of the body.
func messageBody(f *Frame, rows []Row, top, bottom int) {
	msg := f.Message
	if msg == "" {
		msg = "♪"
	}
	y := top + (bottom-top)/2
	if y >= 0 && y < len(rows) {
		rows[y] = centered(f, termx.Truncate(msg, f.W), f.Theme.Dim(), false, false)
	}
}

// activation is the cross-fade amount (0..1) of line i towards the "current"
// colour. The line that just became current fades in, the one that just
// stopped fades out.
func activation(l *lrc.Lyrics, idx, i int, now, fade time.Duration) float64 {
	if idx < 0 {
		return 0
	}
	if fade <= 0 {
		if i == idx {
			return 1
		}
		return 0
	}
	smooth := func(x float64) float64 {
		if x < 0 {
			x = 0
		}
		if x > 1 {
			x = 1
		}
		return x * x * (3 - 2*x)
	}
	p := smooth(float64(now-l.Lines[idx].Time) / float64(fade))
	switch i {
	case idx:
		return p
	case idx - 1:
		return 1 - p
	}
	return 0
}

// wrap breaks s into lines of at most width cells, preferring spaces.
func wrap(s string, width int) []string {
	if width < 1 {
		return []string{s}
	}
	var lines []string
	var cur []rune
	curW, lastSpace := 0, -1
	flush := func(rs []rune) {
		lines = append(lines, strings.TrimRight(string(rs), " "))
	}
	for _, r := range s {
		rw := termx.RuneWidth(r)
		if r == ' ' && curW == 0 {
			continue
		}
		if curW+rw > width && len(cur) > 0 {
			if lastSpace > 0 {
				flush(cur[:lastSpace])
				rest := append([]rune(nil), cur[lastSpace+1:]...)
				cur = rest
				curW = termx.StringWidth(string(cur))
			} else {
				flush(cur)
				cur, curW = nil, 0
			}
			lastSpace = -1
			if r == ' ' {
				continue
			}
		}
		if r == ' ' {
			lastSpace = len(cur)
		}
		cur = append(cur, r)
		curW += rw
	}
	if len(cur) > 0 {
		flush(cur)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}
