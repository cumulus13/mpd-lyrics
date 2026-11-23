// Package ui turns lyric state into terminal rows (layout) and rows into
// escape sequences (screen). Layout is pure and deterministic so it can be
// unit-tested without a terminal.
package ui

import (
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
	"github.com/cumulus13/mpd-lyrics/internal/config"
	"github.com/cumulus13/mpd-lyrics/internal/lrc"
	"github.com/cumulus13/mpd-lyrics/internal/termx"
)

// Caps describes how glyph scaling ("font size") is realised.
type Caps struct {
	Method string // "kitty" | "dec" | "none"
}

// Max is the largest supported scale.
func (c Caps) Max() int {
	switch c.Method {
	case "kitty":
		return 7
	case "dec":
		return 2
	}
	return 1
}

// Clamp limits s to what the terminal can do.
func (c Caps) Clamp(s int) int {
	if s < 1 {
		return 1
	}
	if m := c.Max(); s > m {
		return m
	}
	return s
}

// Seg is a run of text with one style, or a Gap of blank screen cells.
type Seg struct {
	Text     string
	Gap      int // number of blank screen cells (Text must be empty)
	Color    colorx.RGB
	HasColor bool
	Bold     bool
	Dim      bool
}

// Row is one terminal row. Rows with Scale > 1 belong to a group of Scale rows;
// Part is the index inside that group (0 = top).
type Row struct {
	Segs  []Seg
	Scale int
	Part  int
}

func (r Row) scale() int {
	if r.Scale < 1 {
		return 1
	}
	return r.Scale
}

// Width is the number of screen cells the row occupies.
func (r Row) Width() int {
	w := 0
	for _, s := range r.Segs {
		w += s.Gap + termx.StringWidth(s.Text)*r.scale()
	}
	return w
}

// Theme is a parsed config.Style.
type Theme struct {
	Text, Current, FadeTo colorx.RGB
	FontSize              int
	CurrentFontSize       int
	BoldCurrent           bool
	Wipe                  bool
}

// NewTheme parses the colours of a style.
func NewTheme(s config.Style) (Theme, error) {
	var t Theme
	var err error
	if t.Text, err = colorx.Parse(s.TextColor); err != nil {
		return t, err
	}
	if t.Current, err = colorx.Parse(s.CurrentColor); err != nil {
		return t, err
	}
	if t.FadeTo, err = colorx.Parse(s.FadeTo); err != nil {
		return t, err
	}
	t.FontSize, t.CurrentFontSize = s.FontSize, s.CurrentFontSize
	t.BoldCurrent, t.Wipe = s.BoldCurrent, s.Wipe
	return t, nil
}

// Dim is the colour used for chrome (header, hints).
func (t Theme) Dim() colorx.RGB { return colorx.Lerp(t.FadeTo, t.Text, 0.55) }

// Frame is everything needed to lay out one screenful.
type Frame struct {
	W, H  int
	Now   time.Duration // lyric clock (position + offsets)
	Total time.Duration // track length, 0 if unknown

	Playing bool
	Lyrics  *lrc.Lyrics
	Source  string // "lrclib" | "local" | ""
	Title   string
	Artist  string
	Message string // shown instead of lyrics when there are none
	Toast   string // transient footer message

	Mode     string
	OffsetMS int

	Theme  Theme
	Fade   config.Fade
	Ticker config.Ticker
	UI     config.UI
	Caps   Caps
	Mono   bool // no colour available: use bold/dim instead
}
