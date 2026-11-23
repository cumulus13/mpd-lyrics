package ui

import (
	"math"
	"sort"
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
	"github.com/cumulus13/mpd-lyrics/internal/config"
	"github.com/cumulus13/mpd-lyrics/internal/lrc"
	"github.com/cumulus13/mpd-lyrics/internal/termx"
)

// tickerTape is the precomputed horizontal tape of all lyric lines. The tape
// position P(t) is piecewise linear: P advances by exactly one item (text plus
// separator) between the start times of consecutive lines, so the text always
// moves and the active line always starts at the anchor when its time comes.
type tickerTape struct {
	lyr   *lrc.Lyrics
	total time.Duration
	scale int
	sep   string

	items  [][]rune  // text runes followed by separator runes
	nText  []int     // number of text runes in each item
	textW  []int     // text width in cells (scaled)
	cum    []float64 // cum[i] = tape distance to the start of item i; len n+1
	kt     []float64 // line start times in seconds; kt[n] = end of last line
	v0, vN float64   // extrapolation speeds before the first / after the last line
}

func (tp *tickerTape) ensure(l *lrc.Lyrics, total time.Duration, scale int, sep string) {
	if tp.lyr == l && tp.total == total && tp.scale == scale && tp.sep == sep {
		return
	}
	n := len(l.Lines)
	*tp = tickerTape{lyr: l, total: total, scale: scale, sep: sep}
	sepRunes := []rune(sep)
	sepW := termx.StringWidth(sep) * scale
	tp.cum = make([]float64, n+1)
	tp.kt = make([]float64, n+1)
	for i, ln := range l.Lines {
		text := []rune(ln.Display())
		tw := termx.StringWidth(ln.Display()) * scale
		tp.items = append(tp.items, append(append([]rune(nil), text...), sepRunes...))
		tp.nText = append(tp.nText, len(text))
		tp.textW = append(tp.textW, tw)
		tp.cum[i+1] = tp.cum[i] + float64(tw+sepW)
		tp.kt[i] = ln.Time.Seconds()
	}
	tp.kt[n] = l.End(n-1, total).Seconds()
	clamp := func(v float64) float64 { return math.Max(5, math.Min(40, v)) }
	adv := func(i int) float64 { return tp.cum[i+1] - tp.cum[i] }
	tp.v0 = clamp(adv(0) / math.Max(tp.kt[1]-tp.kt[0], 0.2))
	tp.vN = clamp(adv(n-1) / math.Max(tp.kt[n]-tp.kt[n-1], 0.2))
}

// pos returns the tape position (in cells) at time t (seconds).
func (tp *tickerTape) pos(t float64) float64 {
	n := len(tp.items)
	if t <= tp.kt[0] {
		return tp.cum[0] - tp.v0*(tp.kt[0]-t)
	}
	if t >= tp.kt[n] {
		return tp.cum[n] + tp.vN*(t-tp.kt[n])
	}
	j := sort.Search(n+1, func(k int) bool { return tp.kt[k] > t })
	i := j - 1
	dt := tp.kt[i+1] - tp.kt[i]
	if dt <= 1e-6 {
		return tp.cum[i+1]
	}
	return tp.cum[i] + (t-tp.kt[i])/dt*(tp.cum[i+1]-tp.cum[i])
}

// textStart is the screen column of the first text cell of item i.
func (tp *tickerTape) textStart(i int, p float64, w int, t config.Ticker) float64 {
	anchor := math.Round(t.Anchor * float64(w))
	if t.Direction == config.FromLeft {
		return anchor + (p - tp.cum[i]) - float64(tp.textW[i])
	}
	return float64(w) - anchor + tp.cum[i] - p
}

type tcell struct {
	x, w int
	text string
	col  colorx.RGB
	bold bool
	cur  bool
}

func renderTicker(f *Frame, tp *tickerTape, rows []Row, top, bottom int) {
	th := f.Theme
	scale := f.Caps.Clamp(th.CurrentFontSize)
	tp.ensure(f.Lyrics, f.Total, scale, f.Ticker.Separator)
	n := len(tp.items)
	W := f.W
	idx := f.Lyrics.IndexAt(f.Now)
	p := tp.pos(f.Now.Seconds())
	fadeDur := time.Duration(f.Fade.FadeMS) * time.Millisecond
	fromLeft := f.Ticker.Direction == config.FromLeft
	neighbor := colorx.Lerp(th.FadeTo, th.Text, f.Ticker.NeighborAlpha)

	var cells []tcell
	for k := 0; k < n; k++ {
		i := k
		if fromLeft {
			i = n - 1 - k
		}
		x := int(math.Floor(tp.textStart(i, p, W, f.Ticker) + 0.5))
		itemW := 0
		for _, r := range tp.items[i] {
			itemW += termx.RuneWidth(r) * scale
		}
		if x >= W || x+itemW <= 0 {
			continue
		}

		a := activation(f.Lyrics, idx, i, f.Now, fadeDur)
		base := colorx.Lerp(neighbor, th.Current, a)
		bold := th.BoldCurrent && a > 0.5
		isCur := i == idx
		sepCol := colorx.Lerp(th.FadeTo, th.Text, f.Ticker.NeighborAlpha*0.7)

		prog := 0.0
		if isCur && th.Wipe {
			s := f.Lyrics.Lines[idx].Time
			e := f.Lyrics.End(idx, f.Total)
			prog = math.Max(0, math.Min(1, float64(f.Now-s)/float64(e-s)))
		}

		lastEmitted := false
		for ci, r := range tp.items[i] {
			rw := termx.RuneWidth(r)
			if rw == 0 {
				if lastEmitted && len(cells) > 0 {
					cells[len(cells)-1].text += string(r)
				}
				continue
			}
			cw := rw * scale
			lastEmitted = false
			if x >= 0 && x+cw <= W {
				col := base
				isText := ci < tp.nText[i]
				switch {
				case !isText:
					col = sepCol
				case isCur && th.Wipe:
					if float64(ci)/float64(tp.nText[i]) < prog {
						col = th.Current
					} else {
						col = th.Text
					}
				}
				if ed := f.Ticker.EdgeFade; ed > 0 {
					e := x
					if r := W - (x + cw); r < e {
						e = r
					}
					if e < ed {
						col = colorx.Lerp(th.FadeTo, col, float64(e+1)/float64(ed+1))
					}
				}
				cells = append(cells, tcell{x: x, w: cw, text: string(r), col: col, bold: bold && isText, cur: isCur && isText})
				lastEmitted = true
			}
			x += cw
		}
	}

	var segs []Seg
	cursor, run := 0, -1
	for _, c := range cells {
		if c.x > cursor {
			segs = append(segs, Seg{Gap: c.x - cursor})
			run = -1
		}
		if run >= 0 && segs[run].Color == c.col && segs[run].Bold == c.bold && segs[run].Dim == (f.Mono && !c.cur) {
			segs[run].Text += c.text
		} else {
			s := f.textSeg(c.text, c.col, c.bold, c.cur)
			segs = append(segs, s)
			run = len(segs) - 1
		}
		cursor = c.x + c.w
	}

	y := top + (bottom-top-scale)/2
	if y < top {
		y = top
	}
	for part := 0; part < scale && y+part < bottom; part++ {
		rows[y+part] = Row{Segs: segs, Scale: scale, Part: part}
	}
}
