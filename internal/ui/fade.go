package ui

import (
	"math"
	"time"
	"unicode/utf8"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
	"github.com/cumulus13/mpd-lyrics/internal/termx"
)

type fadeBlock struct {
	idx   int
	lines []string
	scale int
}

func (b fadeBlock) height() int { return len(b.lines) * b.scale }

// renderFade lays out several lyric lines around the current one. The current
// line is vertically centred; other lines fade with distance and cross-fade
// to/from the highlight colour when the current line changes.
func renderFade(f *Frame, rows []Row, top, bottom int) {
	l := f.Lyrics
	n := len(l.Lines)
	th := f.Theme
	idx := l.IndexAt(f.Now)
	center := idx
	if center < 0 {
		center = 0
	}
	sNorm := f.Caps.Clamp(th.FontSize)
	sCur := f.Caps.Clamp(th.CurrentFontSize)
	fadeDur := time.Duration(f.Fade.FadeMS) * time.Millisecond

	mk := func(i int) fadeBlock {
		s := sNorm
		if i == idx {
			s = sCur
		}
		w := f.W / s
		if w < 4 {
			w = 4
		}
		return fadeBlock{idx: i, lines: wrap(l.Lines[i].Display(), w), scale: s}
	}

	side := f.Fade.Lines / 2
	avail := bottom - top
	cb := mk(center)
	y0 := top + (avail-cb.height())/2
	if y0 < top {
		y0 = top
	}
	place(f, rows, cb, y0, bottom, center, idx, side, fadeDur)

	y := y0
	for d := 1; d <= side; d++ {
		i := center - d
		if i < 0 {
			break
		}
		b := mk(i)
		need := b.height() + f.Fade.Spacing
		if y-need < top {
			break
		}
		y -= need
		place(f, rows, b, y, bottom, center, idx, side, fadeDur)
	}

	y = y0 + cb.height()
	for d := 1; d <= side; d++ {
		i := center + d
		if i >= n {
			break
		}
		b := mk(i)
		if y+f.Fade.Spacing+b.height() > bottom {
			break
		}
		y += f.Fade.Spacing
		place(f, rows, b, y, bottom, center, idx, side, fadeDur)
		y += b.height()
	}
}

func place(f *Frame, rows []Row, b fadeBlock, y, bottom, center, idx, side int, fadeDur time.Duration) {
	th := f.Theme
	d := b.idx - center
	if d < 0 {
		d = -d
	}
	alpha := 1.0
	if side > 0 {
		alpha = 1 - (1-f.Fade.MinAlpha)*float64(d)/float64(side+1)
	}
	base := colorx.Lerp(th.FadeTo, th.Text, alpha)
	a := activation(f.Lyrics, idx, b.idx, f.Now, fadeDur)
	col := colorx.Lerp(base, th.Current, a)
	bold := th.BoldCurrent && a > 0.5
	isCur := b.idx == idx

	// Karaoke wipe progress over the whole (wrapped) current line.
	total, done := 0, 0
	if isCur && th.Wipe {
		for _, ln := range b.lines {
			total += utf8.RuneCountInString(ln)
		}
		start := f.Lyrics.Lines[idx].Time
		end := f.Lyrics.End(idx, f.Total)
		p := float64(f.Now-start) / float64(end-start)
		p = math.Max(0, math.Min(1, p))
		done = int(math.Round(p * float64(total)))
	}

	consumed := 0
	for k, ln := range b.lines {
		row := y + k*b.scale
		if row+b.scale > bottom {
			return
		}
		cells := termx.StringWidth(ln) * b.scale
		pad := 0
		switch f.Fade.Align {
		case "left":
			pad = 1
		default:
			pad = (f.W - cells) / 2
		}
		if pad < 0 {
			pad = 0
		}

		var segs []Seg
		if pad > 0 {
			segs = append(segs, Seg{Gap: pad})
		}
		if isCur && th.Wipe && total > 0 {
			rs := []rune(ln)
			cut := done - consumed
			if cut < 0 {
				cut = 0
			}
			if cut > len(rs) {
				cut = len(rs)
			}
			if cut > 0 {
				segs = append(segs, f.textSeg(string(rs[:cut]), th.Current, th.BoldCurrent, true))
			}
			if cut < len(rs) {
				segs = append(segs, f.textSeg(string(rs[cut:]), th.Text, false, false))
			}
			consumed += len(rs)
		} else {
			segs = append(segs, f.textSeg(ln, col, bold, isCur && a > 0.5))
		}
		for p := 0; p < b.scale; p++ {
			rows[row+p] = Row{Segs: segs, Scale: b.scale, Part: p}
		}
	}
}
