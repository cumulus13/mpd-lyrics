package ui

import "github.com/cumulus13/mpd-lyrics/internal/config"

// Renderer lays out frames. It keeps the ticker tape cached between frames.
type Renderer struct {
	tape tickerTape
}

// Render returns exactly f.H rows.
func (r *Renderer) Render(f *Frame) []Row {
	if f.H < 1 {
		return nil
	}
	rows := make([]Row, f.H)
	if f.W < 4 {
		return rows
	}
	top, bottom := layoutChrome(f, rows)
	if f.Lyrics.Empty() {
		messageBody(f, rows, top, bottom)
		return rows
	}
	switch f.Mode {
	case config.ModeTicker:
		renderTicker(f, &r.tape, rows, top, bottom)
	default:
		renderFade(f, rows, top, bottom)
	}
	return rows
}
