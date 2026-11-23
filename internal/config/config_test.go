package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateParsesToDefaults(t *testing.T) {
	c, err := Parse(Template(), "linux")
	if err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if len(c.Warnings) != 0 {
		t.Fatalf("template has unknown keys: %v", c.Warnings)
	}
	d := Default()
	if c.Mode != d.Mode || c.FPS != d.FPS || c.Styles[ModeFade] != d.Styles[ModeFade] ||
		c.Ticker != d.Ticker || c.Fade != d.Fade || c.LRCLIB != d.LRCLIB || c.MPD != d.MPD || c.UI != d.UI {
		t.Errorf("template values differ from Default():\n%+v\n%+v", c, d)
	}
}

func TestExampleFileMatchesTemplate(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config.example.toml"))
	if err != nil {
		t.Skipf("config.example.toml not present: %v", err)
	}
	if string(b) != Template() {
		t.Error("config.example.toml is out of sync with internal/config/default.toml (run: make sync-config)")
	}
}

func TestDefaultColors(t *testing.T) {
	d := Default()
	s := d.StyleFor(ModeFade)
	if s.TextColor != "green" || s.CurrentColor != "yellow" {
		t.Errorf("default colours = %q/%q, want green/yellow", s.TextColor, s.CurrentColor)
	}
	if d.Ticker.Direction != FromRight {
		t.Errorf("default direction = %q", d.Ticker.Direction)
	}
}

func TestPlatformOverridePrecedence(t *testing.T) {
	src := `
[style]
text_color = "green"
font_size = 1

[platform.windows.style]
text_color = "#112233"

[ticker]
text_color = "cyan"
font_size = 2

[platform.windows.ticker]
font_size = 3

[general]
mode = "text"
fps = 20

[platform.darwin.general]
fps = 45
`
	win, err := Parse(src, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if got := win.StyleFor(ModeFade).TextColor; got != "#112233" {
		t.Errorf("windows fade text = %q", got)
	}
	if got := win.StyleFor(ModeTicker).TextColor; got != "cyan" {
		t.Errorf("mode section must beat platform style: %q", got)
	}
	if got := win.StyleFor(ModeTicker).FontSize; got != 3 {
		t.Errorf("platform.windows.ticker must win: %d", got)
	}
	if win.Mode != ModeTicker || win.FPS != 20 {
		t.Errorf("mode=%q fps=%d", win.Mode, win.FPS)
	}

	mac, err := Parse(src, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if mac.FPS != 45 {
		t.Errorf("darwin fps = %d", mac.FPS)
	}
	if mac.StyleFor(ModeFade).TextColor != "green" {
		t.Errorf("darwin should not see windows override")
	}
	if mac.StyleFor(ModeTicker).FontSize != 2 {
		t.Errorf("darwin ticker font = %d", mac.StyleFor(ModeTicker).FontSize)
	}
}

func TestHighlightAlias(t *testing.T) {
	c, err := Parse("[style]\nhighlight_color = \"#ff0000\"\n", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if c.StyleFor(ModeFade).CurrentColor != "#ff0000" {
		t.Errorf("alias not applied: %+v", c.StyleFor(ModeFade))
	}
}

func TestDirectionAliases(t *testing.T) {
	for in, want := range map[string]string{"right": FromRight, "from_left": FromLeft, "LTR": FromLeft} {
		c, err := Parse("[ticker]\ndirection = \""+in+"\"\n", "linux")
		if err != nil || c.Ticker.Direction != want {
			t.Errorf("%q -> %q, %v", in, c.Ticker.Direction, err)
		}
	}
}

func TestValidationErrors(t *testing.T) {
	bad := map[string]string{
		"mode":      "[general]\nmode = \"disco\"\n",
		"fps":       "[general]\nfps = 0\n",
		"colour":    "[style]\ntext_color = \"nope\"\n",
		"font":      "[style]\nfont_size = 9\n",
		"type":      "[general]\nfps = \"fast\"\n",
		"direction": "[ticker]\ndirection = \"up\"\n",
		"url":       "[lrclib]\nbase_url = \"lrclib.net\"\n",
		"syntax":    "[general\nmode = \"fade\"\n",
		"unquoted":  "[general]\nmode = fade\n",
		"dup":       "[general]\nfps = 1\nfps = 2\n",
	}
	for name, src := range bad {
		if _, err := Parse(src, "linux"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestUnknownKeysWarn(t *testing.T) {
	c, err := Parse("[general]\nbogus = 1\n[platform.linux.fade]\nlines = 5\nzzz = 1\n", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Warnings) != 2 {
		t.Fatalf("warnings = %v", c.Warnings)
	}
	if c.Fade.Lines != 5 {
		t.Errorf("lines = %d", c.Fade.Lines)
	}
}

func TestTOMLValues(t *testing.T) {
	m, err := parseTOML("a = \"x # y\" # c\nb = 'lit\\n'\nc = 1_000\nd = 0.5\ne = true\n[s.t]\nf = \"q\\\"z\"\n")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]any{
		"a": "x # y", "b": `lit\n`, "c": int64(1000), "d": 0.5, "e": true, "s.t.f": `q"z`,
	}
	for k, want := range checks {
		if m[k] != want {
			t.Errorf("%s = %#v want %#v", k, m[k], want)
		}
	}
	if _, err := parseTOML("x = [1,2]\n"); err == nil {
		t.Error("arrays should be rejected")
	}
}

func TestSearchPaths(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	home := filepath.FromSlash("/home/u")

	got := SearchPaths("linux", env(nil), home)
	if len(got) != 1 || got[0] != filepath.Join(home, ".config", "mpd-lyrics", "config.toml") {
		t.Errorf("linux = %v", got)
	}
	got = SearchPaths("linux", env(map[string]string{"XDG_CONFIG_HOME": "/x"}), home)
	if got[0] != filepath.Join("/x", "mpd-lyrics", "config.toml") {
		t.Errorf("linux xdg = %v", got)
	}
	got = SearchPaths("darwin", env(nil), home)
	if len(got) != 2 || !strings.Contains(got[0], "Application Support") {
		t.Errorf("darwin = %v", got)
	}
	got = SearchPaths("windows", env(map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`}), home)
	if len(got) != 1 || !strings.HasSuffix(got[0], filepath.Join("mpd-lyrics", "config.toml")) {
		t.Errorf("windows = %v", got)
	}
}

func TestLoadExplicitMissingFails(t *testing.T) {
	if _, err := LoadFor(filepath.Join(t.TempDir(), "nope.toml"), "linux"); err == nil {
		t.Fatal("expected error for missing explicit path")
	}
}

func TestLoadFromFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte("[general]\nfps = 12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFor(p, "linux")
	if err != nil || c.FPS != 12 || c.Path != p {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}
