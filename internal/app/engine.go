// Package app wires MPD polling, lyric fetching and rendering together.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
	"github.com/cumulus13/mpd-lyrics/internal/config"
	"github.com/cumulus13/mpd-lyrics/internal/lrc"
	"github.com/cumulus13/mpd-lyrics/internal/lrclib"
	"github.com/cumulus13/mpd-lyrics/internal/mpd"
	"github.com/cumulus13/mpd-lyrics/internal/ui"
)

// Options are command-line level switches that are not part of the config file.
type Options struct {
	Mode    string // override general.mode
	NoCache bool
	Pipe    bool // plain text output instead of the full-screen UI
	JSON    bool // with Pipe: Waybar/Polybar style JSON objects
	Once    bool // with Pipe: print the current line once and exit
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
}

const (
	msgFetching = "Fetching lyrics…"
	msgNothing  = "Nothing playing"
)

type snapshot struct {
	st   mpd.Status
	song mpd.Song
	at   time.Time
	err  error
}

type lyricsMsg struct {
	key     string
	lyr     *lrc.Lyrics
	source  string
	message string
}

// clock extrapolates the playback position between MPD polls.
type clock struct {
	base    float64
	at      time.Time
	playing bool
	init    bool
}

func (c *clock) update(st mpd.Status, at time.Time) {
	playing := st.State == "play"
	if c.init && playing && c.playing {
		pred := c.now(at)
		if d := st.Elapsed - pred; math.Abs(d) < 0.25 {
			c.base, c.at = pred+d*0.5, at // small drift: nudge instead of jump
			return
		}
	}
	c.base, c.at, c.playing, c.init = st.Elapsed, at, playing, true
}

func (c *clock) now(t time.Time) float64 {
	if !c.playing {
		return c.base
	}
	return c.base + t.Sub(c.at).Seconds()
}

type engine struct {
	cfg *config.Config
	opt Options
	ctx context.Context

	client  *lrclib.Client
	profile colorx.Profile
	caps    ui.Caps

	snaps chan snapshot
	cmds  chan string
	lyr   chan lyricsMsg

	// model
	mode        string
	themes      map[string]ui.Theme
	showHelp    bool
	clock       clock
	status      mpd.Status
	song        mpd.Song
	songKey     string
	gotSnap     bool
	connErr     error
	lyrics      *lrc.Lyrics
	source      string
	message     string
	extraOffset time.Duration
	toast       string
	toastUntil  time.Time
	cancelFetch context.CancelFunc
}

func newEngine(cfg *config.Config, opt Options) (*engine, error) {
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Stderr == nil {
		opt.Stderr = os.Stderr
	}
	e := &engine{
		cfg:    cfg,
		opt:    opt,
		snaps:  make(chan snapshot, 1),
		cmds:   make(chan string, 8),
		lyr:    make(chan lyricsMsg, 1),
		mode:   cfg.Mode,
		themes: map[string]ui.Theme{},
	}
	if opt.Mode != "" {
		e.mode = opt.Mode
	}
	for _, m := range []string{config.ModeFade, config.ModeTicker} {
		th, err := ui.NewTheme(cfg.StyleFor(m))
		if err != nil {
			return nil, fmt.Errorf("%s style: %w", m, err)
		}
		e.themes[m] = th
	}
	e.showHelp = cfg.UI.ShowHelp
	e.profile = resolveProfile(cfg)
	e.caps = resolveCaps(cfg)

	ua := "mpd-lyrics/" + opt.Version + " (https://github.com/cumulus13/mpd-lyrics)"
	e.client = &lrclib.Client{
		BaseURL:     cfg.LRCLIB.BaseURL,
		UserAgent:   ua,
		HTTP:        &http.Client{Timeout: time.Duration(cfg.LRCLIB.TimeoutMS) * time.Millisecond},
		TTL:         time.Duration(cfg.LRCLIB.CacheTTLDays) * 24 * time.Hour,
		NotFoundTTL: time.Duration(cfg.LRCLIB.NotFoundTTLHours) * time.Hour,
		MaxDurDiff:  time.Duration(cfg.LRCLIB.MaxDurationDiffS) * time.Second,
	}
	if cfg.LRCLIB.Cache && !opt.NoCache {
		dir := cfg.LRCLIB.CacheDir
		if dir == "" {
			if base, err := os.UserCacheDir(); err == nil {
				dir = filepath.Join(base, "mpd-lyrics")
			}
		}
		if dir != "" {
			e.client.Cache = &lrclib.Cache{Dir: dir}
		}
	}
	return e, nil
}

func resolveProfile(cfg *config.Config) colorx.Profile {
	if p, ok, _ := colorx.ParseProfile(cfg.ColorMode); ok {
		return p
	}
	return colorx.Detect(os.Getenv, runtime.GOOS == "windows")
}

func resolveCaps(cfg *config.Config) ui.Caps {
	m := cfg.ScaleMethod
	if m == "auto" {
		if os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(os.Getenv("TERM"), "kitty") {
			m = "kitty"
		} else {
			m = "none"
		}
	}
	return ui.Caps{Method: m}
}

func (e *engine) totalOffset() time.Duration {
	return time.Duration(e.cfg.OffsetMS)*time.Millisecond + e.extraOffset
}

func (e *engine) say(format string, a ...any) {
	e.toast = fmt.Sprintf(format, a...)
	e.toastUntil = time.Now().Add(2 * time.Second)
}

// ---------------------------------------------------------------------------
// MPD polling

func (e *engine) send(s snapshot) {
	select {
	case e.snaps <- s:
		return
	default:
	}
	select { // drop the stale snapshot
	case <-e.snaps:
	default:
	}
	select {
	case e.snaps <- s:
	default:
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (e *engine) runPoller(ctx context.Context) {
	network, addr, pass := mpd.Resolve(e.cfg.MPD.Host, e.cfg.MPD.Port, e.cfg.MPD.Password, os.Getenv)
	timeout := time.Duration(e.cfg.MPD.TimeoutMS) * time.Millisecond
	backoff := time.Second
	for ctx.Err() == nil {
		c, err := mpd.Dial(network, addr, pass, timeout)
		if err != nil {
			e.send(snapshot{err: fmt.Errorf("MPD %s: %w", addr, err)})
			sleepCtx(ctx, backoff)
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		e.pollLoop(ctx, c)
		c.Close()
	}
}

func (e *engine) pollLoop(ctx context.Context, c *mpd.Client) {
	t := time.NewTicker(time.Duration(e.cfg.PollMS) * time.Millisecond)
	defer t.Stop()
	poll := func() bool {
		st, song, err := c.Poll()
		if err != nil {
			e.send(snapshot{err: err})
			return false
		}
		e.send(snapshot{st: st, song: song, at: time.Now()})
		return true
	}
	if !poll() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-e.cmds:
			switch cmd {
			case "pause":
				_ = c.Pause()
			case "next":
				_ = c.Next()
			case "prev":
				_ = c.Previous()
			}
			if !poll() {
				return
			}
		case <-t.C:
			if !poll() {
				return
			}
		}
	}
}

func (e *engine) onSnapshot(s snapshot) {
	if s.err != nil {
		e.connErr = s.err
		return
	}
	e.connErr = nil
	e.gotSnap = true
	e.status, e.song = s.st, s.song
	e.clock.update(s.st, s.at)

	key := ""
	if !s.song.Empty() && s.st.State != "stop" {
		key = s.song.Key()
	}
	if key != e.songKey {
		e.songKey = key
		e.startFetch(s.song, false)
	}
}

// ---------------------------------------------------------------------------
// Lyrics

func (e *engine) startFetch(song mpd.Song, refresh bool) {
	if e.cancelFetch != nil {
		e.cancelFetch()
		e.cancelFetch = nil
	}
	e.lyrics, e.source = nil, ""
	if e.songKey == "" {
		e.message = msgNothing
		return
	}
	e.message = msgFetching
	ctx, cancel := context.WithCancel(e.ctx)
	e.cancelFetch = cancel
	key := e.songKey
	go func() {
		m := e.fetch(ctx, song, refresh)
		m.key = key
		select {
		case e.lyr <- m:
		case <-ctx.Done():
		}
	}()
}

func (e *engine) onLyrics(m lyricsMsg) {
	if m.key != e.songKey {
		return
	}
	e.lyrics, e.source, e.message = m.lyr, m.source, m.message
}

func (e *engine) fetch(ctx context.Context, song mpd.Song, refresh bool) lyricsMsg {
	total := time.Duration(song.Duration * float64(time.Second))

	if e.cfg.LRCLIB.PreferLocal {
		if l := localLRC(e.cfg.MPD.MusicDir, song.File); l != nil {
			return lyricsMsg{lyr: l, source: "local"}
		}
	}

	tr := lrclib.Track{Title: song.Title, Artist: song.Artist, Album: song.Album, Duration: total}
	res, err := e.client.Lookup(ctx, tr, refresh)
	switch {
	case errors.Is(err, lrclib.ErrNotFound):
		if !e.cfg.LRCLIB.PreferLocal {
			if l := localLRC(e.cfg.MPD.MusicDir, song.File); l != nil {
				return lyricsMsg{lyr: l, source: "local"}
			}
		}
		return lyricsMsg{message: "No lyrics found"}
	case err != nil:
		if ctx.Err() != nil {
			return lyricsMsg{message: msgFetching}
		}
		return lyricsMsg{message: "Lyrics lookup failed (" + shorten(err.Error()) + ")"}
	}

	if res.SyncedLyrics != "" {
		if l := lrc.Parse(res.SyncedLyrics); !l.Empty() {
			return lyricsMsg{lyr: l, source: "lrclib"}
		}
	}
	if res.Instrumental && res.PlainLyrics == "" {
		return lyricsMsg{message: "♪ Instrumental"}
	}
	if res.PlainLyrics != "" && e.cfg.LRCLIB.UnsyncedFallback == "estimate" && total > 0 {
		if l := lrc.FromPlain(res.PlainLyrics); !l.Empty() {
			return lyricsMsg{lyr: lrc.Estimate(l, total), source: "lrclib"}
		}
	}
	return lyricsMsg{message: "Only unsynced lyrics available"}
}

func shorten(s string) string {
	s = strings.TrimPrefix(s, "lrclib: ")
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

// localLRC looks for "<music_dir>/<file without ext>.lrc".
func localLRC(musicDir, file string) *lrc.Lyrics {
	if musicDir == "" || file == "" || strings.Contains(file, "://") {
		return nil
	}
	rel := filepath.Clean(filepath.FromSlash(file))
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil
	}
	p := filepath.Join(musicDir, strings.TrimSuffix(rel, filepath.Ext(rel))+".lrc")
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() || fi.Size() > 2<<20 {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	if l := lrc.Parse(string(b)); !l.Empty() {
		return l
	}
	return nil
}

// ---------------------------------------------------------------------------
// Derived state

// lyricTime is the position used to look up the active lyric line.
func (e *engine) lyricTime(now time.Time) time.Duration {
	t := time.Duration((e.clock.now(now) + e.totalOffset().Seconds()) * float64(time.Second))
	if t < 0 {
		t = 0
	}
	return t
}

func (e *engine) statusMessage() string {
	switch {
	case e.connErr != nil && !e.gotSnap:
		return "Connecting to MPD… (" + shorten(e.connErr.Error()) + ")"
	case e.connErr != nil:
		return "Reconnecting to MPD…"
	case e.songKey == "":
		return msgNothing
	}
	return e.message
}

func (e *engine) frame(w, h int, now time.Time) *ui.Frame {
	f := &ui.Frame{
		W: w, H: h,
		Now:      e.lyricTime(now),
		Total:    time.Duration(e.song.Duration * float64(time.Second)),
		Playing:  e.status.State == "play",
		Lyrics:   e.lyrics,
		Source:   e.source,
		Title:    e.song.Title,
		Artist:   e.song.Artist,
		Message:  e.statusMessage(),
		Mode:     e.mode,
		OffsetMS: int(e.totalOffset() / time.Millisecond),
		Theme:    e.themes[e.mode],
		Fade:     e.cfg.Fade,
		Ticker:   e.cfg.Ticker,
		UI:       e.cfg.UI,
		Caps:     e.caps,
		Mono:     e.profile == colorx.None,
	}
	f.UI.ShowHelp = e.showHelp
	if now.Before(e.toastUntil) {
		f.Toast = e.toast
	}
	// Ticker mode uses the cross-fade duration of fade mode.
	return f
}
