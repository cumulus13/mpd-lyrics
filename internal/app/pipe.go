package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/config"
)

// pipeLine is what -pipe emits: the active lyric text plus metadata.
type pipeLine struct {
	Text    string `json:"text"`
	Tooltip string `json:"tooltip"`
	Class   string `json:"class"`
	Alt     string `json:"alt"`
}

func (e *engine) pipeLine(now time.Time) pipeLine {
	p := pipeLine{}
	if e.song.Title != "" {
		p.Tooltip = e.song.Title
		if e.song.Artist != "" {
			p.Tooltip = e.song.Artist + " — " + e.song.Title
		}
	}
	switch {
	case e.connErr != nil:
		p.Class = "disconnected"
	case e.songKey == "":
		p.Class = "stopped"
	case e.lyrics.Empty():
		p.Class = "nolyrics"
	case e.status.State != "play":
		p.Class = "paused"
	default:
		p.Class = "playing"
	}
	if !e.lyrics.Empty() && e.songKey != "" {
		if i := e.lyrics.IndexAt(e.lyricTime(now)); i >= 0 {
			p.Text = e.lyrics.Lines[i].Text
		}
	}
	p.Alt = p.Class
	return p
}

// pipeReady reports whether enough state is known to answer -once.
func (e *engine) pipeReady() bool {
	if e.connErr != nil || !e.gotSnap {
		return false
	}
	return e.songKey == "" || e.lyrics != nil || (e.message != msgFetching && e.message != "")
}

func (e *engine) runPipe(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	e.ctx = ctx
	go e.runPoller(ctx)

	var deadline <-chan time.Time
	if e.opt.Once {
		t := time.NewTimer(15 * time.Second)
		defer t.Stop()
		deadline = t.C
	}

	enc := json.NewEncoder(e.opt.Stdout)
	var last pipeLine
	first := true
	emit := func(now time.Time) {
		p := e.pipeLine(now)
		if !first && p == last {
			return
		}
		first, last = false, p
		if e.opt.JSON {
			_ = enc.Encode(p)
		} else {
			fmt.Fprintln(e.opt.Stdout, p.Text)
		}
	}

	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			if e.connErr != nil {
				return e.connErr
			}
			return errors.New("timed out waiting for MPD and lyrics")
		case s := <-e.snaps:
			e.onSnapshot(s)
		case m := <-e.lyr:
			e.onLyrics(m)
		case now := <-tick.C:
			if e.opt.Once {
				if e.pipeReady() {
					emit(now)
					return nil
				}
				continue
			}
			if e.gotSnap || e.connErr != nil {
				emit(now)
			}
		}
	}
}

// Doctor prints a diagnostic report: config, MPD connectivity and a lyric
// lookup for the current song.
func Doctor(ctx context.Context, cfg *config.Config, opt Options) error {
	e, err := newEngine(cfg, opt)
	if err != nil {
		return err
	}
	out := e.opt.Stdout
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	e.ctx = cctx

	src := cfg.Path
	if src == "" {
		src = "(built-in defaults, no config file found)"
	}
	fmt.Fprintf(out, "config:        %s\n", src)
	for _, w := range cfg.Warnings {
		fmt.Fprintf(out, "  warning:     %s\n", w)
	}
	fmt.Fprintf(out, "mode:          %s\n", e.mode)
	fmt.Fprintf(out, "colour:        profile %d (0=none 1=16 2=256 3=truecolor)\n", e.profile)
	fmt.Fprintf(out, "font scaling:  %s (max scale %d)\n", e.caps.Method, e.caps.Max())
	fmt.Fprintf(out, "lrclib:        %s (cache: %v)\n", cfg.LRCLIB.BaseURL, e.client.Cache != nil)

	go e.runPoller(cctx)
	var snap snapshot
	for {
		select {
		case <-cctx.Done():
			if e.connErr != nil {
				fmt.Fprintf(out, "mpd:           FAILED: %v\n", e.connErr)
			} else {
				fmt.Fprintln(out, "mpd:           FAILED: timed out")
			}
			return errors.New("doctor: MPD not reachable")
		case snap = <-e.snaps:
		}
		if snap.err != nil {
			e.connErr = snap.err
			continue
		}
		break
	}
	fmt.Fprintf(out, "mpd:           connected, state=%s\n", snap.st.State)
	if snap.song.Empty() {
		fmt.Fprintln(out, "song:          nothing loaded; play something and re-run")
		return nil
	}
	fmt.Fprintf(out, "song:          %s — %s [%s] %.0fs\n", snap.song.Artist, snap.song.Title, snap.song.Album, snap.song.Duration)

	e.songKey = snap.song.Key()
	m := e.fetch(cctx, snap.song, false)
	switch {
	case m.lyr != nil:
		kind := "synced"
		if m.lyr.Estimated {
			kind = "estimated from plain text"
		}
		fmt.Fprintf(out, "lyrics:        OK from %s, %d lines, %s\n", m.source, len(m.lyr.Lines), kind)
	default:
		fmt.Fprintf(out, "lyrics:        %s\n", m.message)
	}
	return nil
}
