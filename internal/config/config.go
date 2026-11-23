// Package config loads mpd-lyrics settings from a TOML-subset file with
// per-platform overrides, applies defaults and validates every value.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
)

//go:embed default.toml
var defaultTOML string

// Template returns the fully commented default configuration file.
func Template() string { return defaultTOML }

// Display modes.
const (
	ModeFade   = "fade"
	ModeTicker = "ticker"
)

// Directions of the ticker tape.
const (
	FromRight = "from_right"
	FromLeft  = "from_left"
)

// MPD holds connection settings.
type MPD struct {
	Host      string
	Port      int
	Password  string
	TimeoutMS int
	MusicDir  string
}

// LRCLIB holds lyric provider settings.
type LRCLIB struct {
	BaseURL          string
	TimeoutMS        int
	Cache            bool
	CacheDir         string
	CacheTTLDays     int
	NotFoundTTLHours int
	MaxDurationDiffS int
	UnsyncedFallback string // "estimate" | "off"
	PreferLocal      bool
}

// Style is the resolved look of one display mode.
type Style struct {
	TextColor       string
	CurrentColor    string
	FadeTo          string
	FontSize        int
	CurrentFontSize int
	BoldCurrent     bool
	Wipe            bool
}

// Ticker holds ticker-mode settings.
type Ticker struct {
	Direction     string
	Anchor        float64
	Separator     string
	EdgeFade      int
	NeighborAlpha float64
}

// Fade holds fade-mode settings.
type Fade struct {
	Lines    int
	Spacing  int
	FadeMS   int
	MinAlpha float64
	Align    string
}

// UI holds chrome settings.
type UI struct {
	ShowTitle  bool
	ShowFooter bool
	ShowHelp   bool
}

// Config is the complete, validated configuration.
type Config struct {
	Mode        string
	FPS         int
	PollMS      int
	OffsetMS    int
	ColorMode   string
	ScaleMethod string

	MPD    MPD
	LRCLIB LRCLIB
	Styles map[string]Style // keyed by ModeFade / ModeTicker
	Ticker Ticker
	Fade   Fade
	UI     UI

	Path     string   // file the config was read from ("" = built-in defaults)
	Warnings []string // non-fatal problems such as unknown keys
}

// StyleFor returns the resolved style of a mode.
func (c *Config) StyleFor(mode string) Style { return c.Styles[mode] }

// Default returns the built-in configuration.
func Default() *Config {
	st := Style{
		TextColor:       "green",
		CurrentColor:    "yellow",
		FadeTo:          "#000000",
		FontSize:        1,
		CurrentFontSize: 1,
		BoldCurrent:     true,
	}
	return &Config{
		Mode:        ModeFade,
		FPS:         30,
		PollMS:      300,
		ColorMode:   "auto",
		ScaleMethod: "auto",
		MPD:         MPD{TimeoutMS: 3000},
		LRCLIB: LRCLIB{
			BaseURL:          "https://lrclib.net",
			TimeoutMS:        8000,
			Cache:            true,
			CacheTTLDays:     30,
			NotFoundTTLHours: 24,
			MaxDurationDiffS: 8,
			UnsyncedFallback: "estimate",
			PreferLocal:      true,
		},
		Styles: map[string]Style{ModeFade: st, ModeTicker: st},
		Ticker: Ticker{Direction: FromRight, Anchor: 0.3, Separator: "   ♪   ", EdgeFade: 8, NeighborAlpha: 0.45},
		Fade:   Fade{Lines: 9, Spacing: 1, FadeMS: 400, MinAlpha: 0.15, Align: "center"},
		UI:     UI{ShowTitle: true, ShowFooter: true},
	}
}

// ---------------------------------------------------------------------------
// Loading

// SearchPaths lists candidate config locations for a platform, best first.
func SearchPaths(goos string, getenv func(string) string, home string) []string {
	xdg := getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	xdgPath := filepath.Join(xdg, "mpd-lyrics", "config.toml")
	switch goos {
	case "windows":
		appdata := getenv("APPDATA")
		if appdata == "" {
			appdata = filepath.Join(home, "AppData", "Roaming")
		}
		return []string{filepath.Join(appdata, "mpd-lyrics", "config.toml")}
	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Application Support", "mpd-lyrics", "config.toml"),
			xdgPath,
		}
	}
	return []string{xdgPath}
}

// DefaultPath is where "-init" writes a new config for the running platform.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return SearchPaths(runtime.GOOS, os.Getenv, home)[0]
}

// Load reads the config for the running platform. An explicit path (or
// $MPD_LYRICS_CONFIG) must exist; otherwise the search paths are tried and the
// defaults are used if none exists.
func Load(path string) (*Config, error) { return LoadFor(path, runtime.GOOS) }

// LoadFor is Load for an arbitrary platform name (used by tests and -check).
func LoadFor(path, goos string) (*Config, error) {
	if path == "" {
		path = os.Getenv("MPD_LYRICS_CONFIG")
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		c, err := Parse(string(b), goos)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		c.Path = path
		return c, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range SearchPaths(goos, os.Getenv, home) {
		b, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read config: %w", err)
		}
		c, err := Parse(string(b), goos)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		c.Path = p
		return c, nil
	}
	return Default(), nil
}

// Parse builds a Config from file contents, applying the overrides of goos.
func Parse(src, goos string) (*Config, error) {
	raw, err := parseTOML(src)
	if err != nil {
		return nil, err
	}
	l := &loader{raw: raw, goos: goos}
	c := Default()

	gen := []string{"general"}
	l.str(&c.Mode, gen, "mode")
	l.integer(&c.FPS, gen, "fps")
	l.integer(&c.PollMS, gen, "poll_interval_ms")
	l.integer(&c.OffsetMS, gen, "offset_ms")
	l.str(&c.ColorMode, gen, "color_mode")
	l.str(&c.ScaleMethod, gen, "scale_method")

	m := []string{"mpd"}
	l.str(&c.MPD.Host, m, "host")
	l.integer(&c.MPD.Port, m, "port")
	l.str(&c.MPD.Password, m, "password")
	l.integer(&c.MPD.TimeoutMS, m, "timeout_ms")
	l.str(&c.MPD.MusicDir, m, "music_dir")

	r := []string{"lrclib"}
	l.str(&c.LRCLIB.BaseURL, r, "base_url")
	l.integer(&c.LRCLIB.TimeoutMS, r, "timeout_ms")
	l.boolean(&c.LRCLIB.Cache, r, "cache")
	l.str(&c.LRCLIB.CacheDir, r, "cache_dir")
	l.integer(&c.LRCLIB.CacheTTLDays, r, "cache_ttl_days")
	l.integer(&c.LRCLIB.NotFoundTTLHours, r, "not_found_ttl_hours")
	l.integer(&c.LRCLIB.MaxDurationDiffS, r, "max_duration_diff_s")
	l.str(&c.LRCLIB.UnsyncedFallback, r, "unsynced_fallback")
	l.boolean(&c.LRCLIB.PreferLocal, r, "prefer_local")

	for _, mode := range []string{ModeFade, ModeTicker} {
		s := c.Styles[mode]
		secs := []string{mode, "style"}
		l.str(&s.TextColor, secs, "text_color")
		l.str(&s.CurrentColor, secs, "current_color", "highlight_color")
		l.str(&s.FadeTo, secs, "fade_to")
		l.integer(&s.FontSize, secs, "font_size")
		l.integer(&s.CurrentFontSize, secs, "current_font_size")
		l.boolean(&s.BoldCurrent, secs, "bold_current")
		l.boolean(&s.Wipe, secs, "wipe")
		c.Styles[mode] = s
	}

	t := []string{"ticker"}
	l.str(&c.Ticker.Direction, t, "direction")
	l.float(&c.Ticker.Anchor, t, "anchor")
	l.str(&c.Ticker.Separator, t, "separator")
	l.integer(&c.Ticker.EdgeFade, t, "edge_fade")
	l.float(&c.Ticker.NeighborAlpha, t, "neighbor_alpha")

	f := []string{"fade"}
	l.integer(&c.Fade.Lines, f, "lines")
	l.integer(&c.Fade.Spacing, f, "spacing")
	l.integer(&c.Fade.FadeMS, f, "fade_ms")
	l.float(&c.Fade.MinAlpha, f, "min_alpha")
	l.str(&c.Fade.Align, f, "align")

	u := []string{"ui"}
	l.boolean(&c.UI.ShowTitle, u, "show_title")
	l.boolean(&c.UI.ShowFooter, u, "show_footer")
	l.boolean(&c.UI.ShowHelp, u, "show_help")

	c.validate(l)
	if len(l.errs) > 0 {
		return nil, errors.New(strings.Join(l.errs, "; "))
	}
	c.Warnings = unknownKeys(raw)
	return c, nil
}

// ---------------------------------------------------------------------------
// Validation

func (c *Config) validate(l *loader) {
	switch strings.ToLower(c.Mode) {
	case "fade":
		c.Mode = ModeFade
	case "ticker", "text":
		c.Mode = ModeTicker
	default:
		l.errf("general.mode: %q is not fade or ticker", c.Mode)
	}
	rng := func(name string, v, lo, hi int) {
		if v < lo || v > hi {
			l.errf("%s: %d out of range %d-%d", name, v, lo, hi)
		}
	}
	rngf := func(name string, v, lo, hi float64) {
		if v < lo || v > hi {
			l.errf("%s: %g out of range %g-%g", name, v, lo, hi)
		}
	}
	rng("general.fps", c.FPS, 1, 120)
	rng("general.poll_interval_ms", c.PollMS, 50, 5000)
	rng("general.offset_ms", c.OffsetMS, -60000, 60000)
	if _, _, err := colorx.ParseProfile(c.ColorMode); err != nil {
		l.errf("general.%v", err)
	}
	switch strings.ToLower(c.ScaleMethod) {
	case "auto", "kitty", "dec", "none":
		c.ScaleMethod = strings.ToLower(c.ScaleMethod)
	default:
		l.errf("general.scale_method: %q is not auto|kitty|dec|none", c.ScaleMethod)
	}
	rng("mpd.port", c.MPD.Port, 0, 65535)
	rng("mpd.timeout_ms", c.MPD.TimeoutMS, 100, 60000)
	rng("lrclib.timeout_ms", c.LRCLIB.TimeoutMS, 500, 120000)
	rng("lrclib.cache_ttl_days", c.LRCLIB.CacheTTLDays, 0, 3650)
	rng("lrclib.not_found_ttl_hours", c.LRCLIB.NotFoundTTLHours, 0, 24*365)
	rng("lrclib.max_duration_diff_s", c.LRCLIB.MaxDurationDiffS, 0, 600)
	if !strings.HasPrefix(c.LRCLIB.BaseURL, "http://") && !strings.HasPrefix(c.LRCLIB.BaseURL, "https://") {
		l.errf("lrclib.base_url: must start with http:// or https://")
	}
	c.LRCLIB.BaseURL = strings.TrimRight(c.LRCLIB.BaseURL, "/")
	switch strings.ToLower(c.LRCLIB.UnsyncedFallback) {
	case "estimate", "off":
		c.LRCLIB.UnsyncedFallback = strings.ToLower(c.LRCLIB.UnsyncedFallback)
	default:
		l.errf("lrclib.unsynced_fallback: %q is not estimate|off", c.LRCLIB.UnsyncedFallback)
	}

	for _, mode := range []string{ModeFade, ModeTicker} {
		s := c.Styles[mode]
		for name, v := range map[string]string{"text_color": s.TextColor, "current_color": s.CurrentColor, "fade_to": s.FadeTo} {
			if _, err := colorx.Parse(v); err != nil {
				l.errf("%s.%s: %v", mode, name, err)
			}
		}
		rng(mode+".font_size", s.FontSize, 1, 7)
		rng(mode+".current_font_size", s.CurrentFontSize, 1, 7)
	}

	switch strings.ToLower(c.Ticker.Direction) {
	case "from_right", "right", "rtl":
		c.Ticker.Direction = FromRight
	case "from_left", "left", "ltr":
		c.Ticker.Direction = FromLeft
	default:
		l.errf("ticker.direction: %q is not from_right|from_left", c.Ticker.Direction)
	}
	rngf("ticker.anchor", c.Ticker.Anchor, 0, 0.9)
	rng("ticker.edge_fade", c.Ticker.EdgeFade, 0, 50)
	rngf("ticker.neighbor_alpha", c.Ticker.NeighborAlpha, 0, 1)

	rng("fade.lines", c.Fade.Lines, 1, 41)
	rng("fade.spacing", c.Fade.Spacing, 0, 3)
	rng("fade.fade_ms", c.Fade.FadeMS, 0, 5000)
	rngf("fade.min_alpha", c.Fade.MinAlpha, 0, 1)
	switch strings.ToLower(c.Fade.Align) {
	case "center", "left":
		c.Fade.Align = strings.ToLower(c.Fade.Align)
	default:
		l.errf("fade.align: %q is not center|left", c.Fade.Align)
	}
}

// ---------------------------------------------------------------------------
// Key bookkeeping

var baseKeys = map[string]bool{}

var styleKeys = map[string]bool{
	"text_color": true, "current_color": true, "highlight_color": true, "fade_to": true,
	"font_size": true, "current_font_size": true, "bold_current": true, "wipe": true,
}

func init() {
	for _, k := range []string{
		"general.mode", "general.fps", "general.poll_interval_ms", "general.offset_ms",
		"general.color_mode", "general.scale_method",
		"mpd.host", "mpd.port", "mpd.password", "mpd.timeout_ms", "mpd.music_dir",
		"lrclib.base_url", "lrclib.timeout_ms", "lrclib.cache", "lrclib.cache_dir",
		"lrclib.cache_ttl_days", "lrclib.not_found_ttl_hours", "lrclib.max_duration_diff_s",
		"lrclib.unsynced_fallback", "lrclib.prefer_local",
		"ticker.direction", "ticker.anchor", "ticker.separator", "ticker.edge_fade", "ticker.neighbor_alpha",
		"fade.lines", "fade.spacing", "fade.fade_ms", "fade.min_alpha", "fade.align",
		"ui.show_title", "ui.show_footer", "ui.show_help",
	} {
		baseKeys[k] = true
	}
	for k := range styleKeys {
		baseKeys["style."+k] = true
	}
}

func isKnownKey(key string) bool {
	parts := strings.Split(key, ".")
	if parts[0] == "platform" {
		if len(parts) < 4 {
			return false
		}
		parts = parts[2:]
	}
	if len(parts) != 2 {
		return false
	}
	if baseKeys[parts[0]+"."+parts[1]] {
		return true
	}
	return (parts[0] == "ticker" || parts[0] == "fade") && styleKeys[parts[1]]
}

func unknownKeys(raw map[string]any) []string {
	var out []string
	for k := range raw {
		if !isKnownKey(k) {
			out = append(out, fmt.Sprintf("unknown config key %q (ignored)", k))
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Typed lookup with platform precedence

type loader struct {
	raw  map[string]any
	goos string
	errs []string
}

func (l *loader) errf(format string, a ...any) { l.errs = append(l.errs, fmt.Sprintf(format, a...)) }

// find walks sections in priority order; within a section the platform
// override wins over the plain key.
func (l *loader) find(sections []string, names []string) (any, string, bool) {
	for _, sec := range sections {
		for _, pre := range []string{"platform." + l.goos + ".", ""} {
			for _, n := range names {
				k := pre + sec + "." + n
				if v, ok := l.raw[k]; ok {
					return v, k, true
				}
			}
		}
	}
	return nil, "", false
}

func (l *loader) str(dst *string, sections []string, names ...string) {
	v, key, ok := l.find(sections, names)
	if !ok {
		return
	}
	s, isStr := v.(string)
	if !isStr {
		l.errf("%s: expected a quoted string", key)
		return
	}
	*dst = s
}

func (l *loader) integer(dst *int, sections []string, names ...string) {
	v, key, ok := l.find(sections, names)
	if !ok {
		return
	}
	n, isInt := v.(int64)
	if !isInt {
		l.errf("%s: expected an integer", key)
		return
	}
	*dst = int(n)
}

func (l *loader) float(dst *float64, sections []string, names ...string) {
	v, key, ok := l.find(sections, names)
	if !ok {
		return
	}
	switch n := v.(type) {
	case int64:
		*dst = float64(n)
	case float64:
		*dst = n
	default:
		l.errf("%s: expected a number", key)
	}
}

func (l *loader) boolean(dst *bool, sections []string, names ...string) {
	v, key, ok := l.find(sections, names)
	if !ok {
		return
	}
	b, isBool := v.(bool)
	if !isBool {
		l.errf("%s: expected true or false", key)
		return
	}
	*dst = b
}
