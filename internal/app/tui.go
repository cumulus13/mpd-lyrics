package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/config"
	"github.com/cumulus13/mpd-lyrics/internal/termx"
	"github.com/cumulus13/mpd-lyrics/internal/ui"
)

// Run starts mpd-lyrics until ctx is cancelled or the user quits.
func Run(ctx context.Context, cfg *config.Config, opt Options) error {
	e, err := newEngine(cfg, opt)
	if err != nil {
		return err
	}
	if opt.Pipe {
		return e.runPipe(ctx)
	}
	return e.runTUI(ctx)
}

func (e *engine) runTUI(parent context.Context) error {
	if !termx.IsTerminal(os.Stdout) {
		return errors.New("stdout is not a terminal; use -pipe for plain text output")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	e.ctx = ctx

	_ = termx.EnableVT()
	restore, rawErr := termx.MakeRaw()
	if rawErr == nil {
		defer restore()
	}
	scr := ui.NewScreen(os.Stdout, e.profile, e.caps)
	if err := scr.Enter(); err != nil {
		return err
	}
	defer scr.Leave()

	go e.runPoller(ctx)

	var keys <-chan termx.Key
	if rawErr == nil {
		keys = termx.ReadKeys(os.Stdin)
	}

	tick := time.NewTicker(time.Second / time.Duration(e.cfg.FPS))
	defer tick.Stop()

	var rend ui.Renderer
	lastW, lastH := 0, 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case s := <-e.snaps:
			e.onSnapshot(s)
		case m := <-e.lyr:
			e.onLyrics(m)
		case k, ok := <-keys:
			if !ok {
				keys = nil
				continue
			}
			if e.onKey(k, scr) {
				return nil
			}
		case now := <-tick.C:
			w, h := termx.Size()
			if w != lastW || h != lastH {
				scr.Reset()
				lastW, lastH = w, h
			}
			if err := scr.Draw(rend.Render(e.frame(w, h, now))); err != nil {
				return fmt.Errorf("drawing: %w", err)
			}
		}
	}
}

// onKey handles one key press and reports whether to quit.
func (e *engine) onKey(k termx.Key, scr *ui.Screen) bool {
	switch k.Name {
	case "ctrl-c":
		return true
	case "tab":
		e.toggleMode(scr)
		return false
	case "left":
		e.nudge(-100 * time.Millisecond)
		return false
	case "right":
		e.nudge(100 * time.Millisecond)
		return false
	}
	switch k.Rune {
	case 'q', 'Q':
		return true
	case 'm', 'M':
		e.toggleMode(scr)
	case '+', '=':
		e.nudge(100 * time.Millisecond)
	case '-', '_':
		e.nudge(-100 * time.Millisecond)
	case '0':
		e.extraOffset = 0
		e.say("offset reset to %+dms", e.cfg.OffsetMS)
	case '[':
		e.resize(-1, scr)
	case ']':
		e.resize(1, scr)
	case 'd', 'D':
		if e.cfg.Ticker.Direction == config.FromRight {
			e.cfg.Ticker.Direction = config.FromLeft
		} else {
			e.cfg.Ticker.Direction = config.FromRight
		}
		e.say("ticker direction: %s", e.cfg.Ticker.Direction)
	case ' ':
		e.command("pause")
	case 'n', 'N':
		e.command("next")
	case 'p', 'P':
		e.command("prev")
	case 'r', 'R':
		if e.songKey != "" {
			e.startFetch(e.song, true)
			e.say("reloading lyrics from LRCLIB…")
		}
	case '?', 'h':
		e.showHelp = !e.showHelp
	}
	return false
}

func (e *engine) command(c string) {
	select {
	case e.cmds <- c:
	default:
	}
}

func (e *engine) toggleMode(scr *ui.Screen) {
	if e.mode == config.ModeFade {
		e.mode = config.ModeTicker
	} else {
		e.mode = config.ModeFade
	}
	scr.Reset()
	e.say("mode: %s", e.mode)
}

func (e *engine) nudge(d time.Duration) {
	e.extraOffset += d
	e.say("offset %+dms (positive = lyrics earlier)", int(e.totalOffset()/time.Millisecond))
}

func (e *engine) resize(delta int, scr *ui.Screen) {
	max := e.caps.Max()
	if max == 1 {
		e.say("font size needs scale_method = kitty or dec")
		return
	}
	th := e.themes[e.mode]
	n := th.CurrentFontSize + delta
	if n < 1 {
		n = 1
	}
	if n > max {
		n = max
	}
	th.CurrentFontSize = n
	e.themes[e.mode] = th
	scr.Reset()
	e.say("current font size: %d", n)
}
