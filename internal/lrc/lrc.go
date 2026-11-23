// Package lrc parses LRC (timed lyrics) and plain-text lyrics and answers
// "which line is active at time t?" queries.
package lrc

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Line is one lyric line with the time it becomes active.
type Line struct {
	Time time.Duration
	Text string
}

// Display returns the text to show for the line. Blank lines (instrumental
// breaks) are rendered as a music note.
func (l Line) Display() string {
	if strings.TrimSpace(l.Text) == "" {
		return "♪"
	}
	return l.Text
}

// Lyrics is a parsed lyric document.
type Lyrics struct {
	Lines     []Line
	Synced    bool              // true when the timestamps came from the source
	Estimated bool              // true when timestamps were synthesised from plain text
	Meta      map[string]string // [ar:], [ti:], [al:], ... (lower-case keys)
}

var (
	tsRe   = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	metaRe = regexp.MustCompile(`^\[([A-Za-z#]+):([^\]]*)\]\s*$`)
	wordRe = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`)
)

// Parse parses LRC text. Multiple timestamps per line, the [offset:] tag and
// enhanced (word-level) <mm:ss.xx> tags are supported; word tags are stripped.
func Parse(src string) *Lyrics {
	l := &Lyrics{Meta: map[string]string{}}
	src = strings.TrimPrefix(src, "\ufeff")
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")

	for _, raw := range strings.Split(src, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var stamps []time.Duration
		rest := raw
		for {
			m := tsRe.FindStringSubmatch(rest)
			if m == nil {
				break
			}
			stamps = append(stamps, stampToDuration(m[1], m[2], m[3]))
			rest = rest[len(m[0]):]
		}
		if len(stamps) == 0 {
			if m := metaRe.FindStringSubmatch(raw); m != nil {
				l.Meta[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
			}
			continue
		}
		text := cleanText(rest)
		for _, s := range stamps {
			l.Lines = append(l.Lines, Line{Time: s, Text: text})
		}
	}

	if v, ok := l.Meta["offset"]; ok {
		if ms, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(v), "+")); err == nil && ms != 0 {
			// LRC spec: positive offset makes lyrics appear sooner.
			shift := time.Duration(ms) * time.Millisecond
			for i := range l.Lines {
				l.Lines[i].Time -= shift
				if l.Lines[i].Time < 0 {
					l.Lines[i].Time = 0
				}
			}
		}
	}

	sort.SliceStable(l.Lines, func(i, j int) bool { return l.Lines[i].Time < l.Lines[j].Time })
	l.Synced = len(l.Lines) > 0
	return l
}

// FromPlain builds unsynced lyrics (all timestamps zero) from plain text.
func FromPlain(src string) *Lyrics {
	l := &Lyrics{Meta: map[string]string{}}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	for _, ln := range strings.Split(src, "\n") {
		l.Lines = append(l.Lines, Line{Text: strings.TrimSpace(ln)})
	}
	// trim leading/trailing blank lines
	for len(l.Lines) > 0 && l.Lines[0].Text == "" {
		l.Lines = l.Lines[1:]
	}
	for len(l.Lines) > 0 && l.Lines[len(l.Lines)-1].Text == "" {
		l.Lines = l.Lines[:len(l.Lines)-1]
	}
	return l
}

// Estimate spreads the lines of unsynced lyrics across the track duration,
// weighting each line by its length. It is a heuristic and is flagged as such
// through Lyrics.Estimated.
func Estimate(l *Lyrics, total time.Duration) *Lyrics {
	out := &Lyrics{Meta: l.Meta, Estimated: true}
	n := len(l.Lines)
	if n == 0 || total <= 0 {
		out.Lines = append(out.Lines, l.Lines...)
		return out
	}
	start := total / 20
	if start > 10*time.Second {
		start = 10 * time.Second
	}
	span := total*9/10 - start
	if span <= 0 {
		span = total
	}
	weights := make([]float64, n)
	var sum float64
	for i, ln := range l.Lines {
		w := 4.0
		if strings.TrimSpace(ln.Text) != "" {
			w = float64(utf8.RuneCountInString(ln.Text)) + 8
		}
		weights[i] = w
		sum += w
	}
	acc := 0.0
	for i, ln := range l.Lines {
		t := start + time.Duration(acc/sum*float64(span))
		out.Lines = append(out.Lines, Line{Time: t, Text: ln.Text})
		acc += weights[i]
	}
	return out
}

// IndexAt returns the index of the line active at t, or -1 before the first.
func (l *Lyrics) IndexAt(t time.Duration) int {
	if l == nil {
		return -1
	}
	return sort.Search(len(l.Lines), func(i int) bool { return l.Lines[i].Time > t }) - 1
}

// End returns the time line i stops being active. total is the track length
// (0 if unknown).
func (l *Lyrics) End(i int, total time.Duration) time.Duration {
	start := l.Lines[i].Time
	var end time.Duration
	switch {
	case i+1 < len(l.Lines):
		end = l.Lines[i+1].Time
	case total > start:
		end = total
	default:
		end = start + 6*time.Second
	}
	if end < start+200*time.Millisecond {
		end = start + 200*time.Millisecond
	}
	return end
}

// Empty reports whether there are no lines.
func (l *Lyrics) Empty() bool { return l == nil || len(l.Lines) == 0 }

func stampToDuration(min, sec, frac string) time.Duration {
	m, _ := strconv.Atoi(min)
	s, _ := strconv.Atoi(sec)
	d := time.Duration(m)*time.Minute + time.Duration(s)*time.Second
	if frac != "" {
		f, _ := strconv.Atoi(frac)
		ms := f
		for i := len(frac); i < 3; i++ {
			ms *= 10
		}
		d += time.Duration(ms) * time.Millisecond
	}
	return d
}

func cleanText(s string) string {
	s = wordRe.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}
