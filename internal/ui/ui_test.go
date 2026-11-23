package ui

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/cumulus13/mpd-lyrics/internal/colorx"
	"github.com/cumulus13/mpd-lyrics/internal/config"
	"github.com/cumulus13/mpd-lyrics/internal/lrc"
)

const sample = `[00:10.00]First line here
[00:20.00]Second line
[00:30.00]Third line is a bit longer
[00:40.00]Fourth
[00:50.00]Fifth line
`

func testFrame(t *testing.T, mode string, now time.Duration) *Frame {
	t.Helper()
	cfg := config.Default()
	th, err := NewTheme(cfg.StyleFor(mode))
	if err != nil {
		t.Fatal(err)
	}
	return &Frame{
		W: 80, H: 20, Now: now, Total: 60 * time.Second, Playing: true,
		Lyrics: lrc.Parse(sample), Source: "lrclib", Title: "T", Artist: "A",
		Mode: mode, Theme: th, Fade: cfg.Fade, Ticker: cfg.Ticker, UI: cfg.UI,
		Caps: Caps{Method: "none"},
	}
}

func rowText(r Row) string {
	var b strings.Builder
	for _, s := range r.Segs {
		b.WriteString(strings.Repeat(" ", s.Gap))
		b.WriteString(s.Text)
	}
	return b.String()
}

func findRow(rows []Row, sub string) int {
	for i, r := range rows {
		if strings.Contains(rowText(r), sub) {
			return i
		}
	}
	return -1
}

func TestRenderReturnsHRows(t *testing.T) {
	var r Renderer
	for _, h := range []int{1, 3, 5, 6, 20} {
		f := testFrame(t, config.ModeFade, 25*time.Second)
		f.H = h
		if got := len(r.Render(f)); got != h {
			t.Errorf("H=%d: got %d rows", h, got)
		}
	}
}

func TestFadeCurrentLineCentredAndColoured(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeFade, 22*time.Second) // second line, 2s in (> fade_ms)
	rows := r.Render(f)

	cur := findRow(rows, "Second line")
	if cur < 0 {
		t.Fatal("current line not rendered")
	}
	body := f.H - 2 // header + footer
	want := 1 + (body-1)/2
	if cur != want {
		t.Errorf("current row = %d, want centred at %d", cur, want)
	}
	// colour of the current line is the highlight colour
	var got colorx.RGB
	for _, s := range rows[cur].Segs {
		if s.Text != "" {
			got = s.Color
		}
	}
	yellow, _ := colorx.Parse("yellow")
	if got != yellow {
		t.Errorf("current colour = %v, want yellow %v", got, yellow)
	}
	// neighbours exist above and below and are dimmer than plain text
	if findRow(rows, "First line") < 0 || findRow(rows, "Third line") < 0 {
		t.Error("neighbouring lines missing")
	}
	green, _ := colorx.Parse("green")
	for _, s := range rows[findRow(rows, "Fifth")].Segs {
		if s.Text != "" && s.Color == green {
			t.Error("far line should be faded, not full green")
		}
	}
	if !strings.Contains(rowText(rows[0]), "A — T") {
		t.Errorf("header = %q", rowText(rows[0]))
	}
}

func TestFadeCrossfadeInProgress(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeFade, 20*time.Second+200*time.Millisecond) // halfway through 400ms fade
	rows := r.Render(f)
	cur := findRow(rows, "Second line")
	yellow, _ := colorx.Parse("yellow")
	green, _ := colorx.Parse("green")
	c := rows[cur].Segs[len(rows[cur].Segs)-1].Color
	if c == yellow || c == green {
		t.Errorf("mid cross-fade colour should be a blend, got %v", c)
	}
}

func TestFadeBeforeFirstLine(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeFade, 2*time.Second)
	rows := r.Render(f)
	if findRow(rows, "First line") < 0 {
		t.Error("first line should preview before it starts")
	}
}

func TestEmptyLyricsShowsMessage(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeFade, 0)
	f.Lyrics = nil
	f.Message = "Fetching lyrics…"
	if findRow(r.Render(f), "Fetching lyrics") < 0 {
		t.Error("message not shown")
	}
}

func TestWrap(t *testing.T) {
	got := wrap("the quick brown fox jumps over", 10)
	want := []string{"the quick", "brown fox", "jumps over"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q want %q", i, got[i], want[i])
		}
	}
	if got := wrap("abcdefghij", 4); len(got) != 3 || got[0] != "abcd" {
		t.Errorf("hard split = %q", got)
	}
	if got := wrap("中文歌词测试", 6); len(got) != 2 {
		t.Errorf("wide wrap = %q", got)
	}
	if got := wrap("", 10); len(got) != 1 {
		t.Errorf("empty = %q", got)
	}
}

func TestTickerAnchorFromRight(t *testing.T) {
	var tp tickerTape
	f := testFrame(t, config.ModeTicker, 0)
	tp.ensure(f.Lyrics, f.Total, 1, f.Ticker.Separator)
	anchor := int(math.Round(f.Ticker.Anchor * float64(f.W)))
	for i := range f.Lyrics.Lines {
		p := tp.pos(f.Lyrics.Lines[i].Time.Seconds())
		got := tp.textStart(i, p, f.W, f.Ticker)
		if want := float64(f.W - anchor); math.Abs(got-want) > 1e-6 {
			t.Errorf("line %d starts at %v, want %v", i, got, want)
		}
	}
}

func TestTickerAnchorFromLeft(t *testing.T) {
	var tp tickerTape
	f := testFrame(t, config.ModeTicker, 0)
	f.Ticker.Direction = config.FromLeft
	tp.ensure(f.Lyrics, f.Total, 1, f.Ticker.Separator)
	anchor := math.Round(f.Ticker.Anchor * float64(f.W))
	for i := range f.Lyrics.Lines {
		p := tp.pos(f.Lyrics.Lines[i].Time.Seconds())
		end := tp.textStart(i, p, f.W, f.Ticker) + float64(tp.textW[i])
		if math.Abs(end-anchor) > 1e-6 {
			t.Errorf("line %d text ends at %v, want %v", i, end, anchor)
		}
	}
}

func TestTickerDirectionOfMotion(t *testing.T) {
	var tp tickerTape
	f := testFrame(t, config.ModeTicker, 0)
	tp.ensure(f.Lyrics, f.Total, 1, f.Ticker.Separator)
	prevR, prevL := math.Inf(1), math.Inf(-1)
	for ts := 12.0; ts < 18; ts += 0.5 {
		p := tp.pos(ts)
		r := tp.textStart(0, p, f.W, config.Ticker{Direction: config.FromRight, Anchor: 0.3})
		l := tp.textStart(0, p, f.W, config.Ticker{Direction: config.FromLeft, Anchor: 0.3})
		if r >= prevR {
			t.Fatalf("from_right must move left: %v >= %v", r, prevR)
		}
		if l <= prevL {
			t.Fatalf("from_left must move right: %v <= %v", l, prevL)
		}
		prevR, prevL = r, l
	}
	// position is continuous across a line boundary
	a, b := tp.pos(19.999), tp.pos(20.001)
	if math.Abs(a-b) > 0.5 {
		t.Errorf("tape jumps at boundary: %v -> %v", a, b)
	}
	// before the first line and after the last the tape keeps moving
	if tp.pos(0) >= tp.pos(5) || tp.pos(70) <= tp.pos(60) {
		t.Error("pre/post roll must keep moving")
	}
}

func TestTickerRenderShowsCurrentLine(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeTicker, 20*time.Second+500*time.Millisecond)
	rows := r.Render(f)
	cur := findRow(rows, "Second")
	if cur < 0 {
		t.Fatalf("ticker did not render the current line; rows: %q", rowText(rows[f.H/2]))
	}
	if w := rows[cur].Width(); w > f.W {
		t.Errorf("row wider than screen: %d > %d", w, f.W)
	}
	// from_left renders too, and still within the screen
	f.Ticker.Direction = config.FromLeft
	for _, row := range r.Render(f) {
		if row.Width() > f.W {
			t.Errorf("from_left row too wide: %d", row.Width())
		}
	}
}

func TestTickerClipsAtEdges(t *testing.T) {
	var r Renderer
	for ms := 0; ms < 60000; ms += 777 {
		f := testFrame(t, config.ModeTicker, time.Duration(ms)*time.Millisecond)
		for _, dir := range []string{config.FromRight, config.FromLeft} {
			f.Ticker.Direction = dir
			for _, row := range r.Render(f) {
				if row.Width() > f.W {
					t.Fatalf("t=%dms %s: width %d > %d", ms, dir, row.Width(), f.W)
				}
			}
		}
	}
}

func TestScreenDiffAndKitty(t *testing.T) {
	var buf bytes.Buffer
	s := NewScreen(&buf, colorx.TrueColor, Caps{Method: "kitty"})
	row := func(txt string, scale, part int) Row {
		return Row{Segs: []Seg{{Text: txt, HasColor: true, Color: colorx.RGB{R: 1, G: 2, B: 3}}}, Scale: scale, Part: part}
	}
	rows := []Row{row("a", 1, 0), row("big", 2, 0), row("big", 2, 1), row("z", 1, 0)}
	if err := s.Draw(rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "\x1b]66;s=2;big\x1b\\") {
		t.Errorf("kitty OSC 66 missing: %q", out)
	}
	if !strings.Contains(out, "38;2;1;2;3") {
		t.Errorf("truecolor SGR missing: %q", out)
	}
	if strings.Contains(out, "\x1b[3;1H") {
		t.Errorf("continuation row must not be written: %q", out)
	}

	buf.Reset()
	if err := s.Draw(rows); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("identical frame wrote %q", buf.String())
	}

	rows[3] = row("changed", 1, 0)
	if err := s.Draw(rows); err != nil {
		t.Fatal(err)
	}
	out = buf.String()
	if !strings.Contains(out, "\x1b[4;1H") || strings.Contains(out, "\x1b[1;1H") || strings.Contains(out, "\x1b[2J") {
		t.Errorf("expected only row 4 to be rewritten: %q", out)
	}

	// layout change forces a clear
	buf.Reset()
	if err := s.Draw([]Row{row("a", 1, 0), row("b", 1, 0), row("c", 1, 0), row("d", 1, 0)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x1b[2J") {
		t.Error("scale change must clear the screen")
	}
}

func TestScreenDEC(t *testing.T) {
	var buf bytes.Buffer
	s := NewScreen(&buf, colorx.None, Caps{Method: "dec"})
	rows := []Row{
		{Segs: []Seg{{Gap: 4}, {Text: "x"}}, Scale: 2, Part: 0},
		{Segs: []Seg{{Gap: 4}, {Text: "x"}}, Scale: 2, Part: 1},
		{Segs: []Seg{{Text: "n"}}},
	}
	if err := s.Draw(rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"\x1b#3", "\x1b#4", "\x1b#5"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "38;") {
		t.Errorf("no colour expected for profile none: %q", out)
	}
}

func TestScreenNoneIgnoresScale(t *testing.T) {
	var buf bytes.Buffer
	s := NewScreen(&buf, colorx.ANSI256, Caps{Method: "none"})
	if err := s.Draw([]Row{{Segs: []Seg{{Text: "hello", HasColor: true}}, Scale: 3}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b]66") || strings.Contains(buf.String(), "\x1b#") {
		t.Errorf("scaling emitted with method none: %q", buf.String())
	}
}

func TestMonoUsesBoldAndDim(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeFade, 25*time.Second)
	f.Mono = true
	rows := r.Render(f)
	cur := rows[findRow(rows, "Second line")]
	var bold bool
	for _, s := range cur.Segs {
		if s.Text != "" {
			bold = s.Bold && !s.Dim
		}
	}
	if !bold {
		t.Error("current line must be bold in mono mode")
	}
	other := rows[findRow(rows, "Fourth")]
	for _, s := range other.Segs {
		if s.Text != "" && !s.Dim {
			t.Error("non-current lines must be dim in mono mode")
		}
	}
}

func TestScaledFadeReservesRows(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeFade, 25*time.Second)
	f.Caps = Caps{Method: "kitty"}
	f.Theme.CurrentFontSize = 2
	rows := r.Render(f)
	cur := findRow(rows, "Second line")
	if rows[cur].Scale != 2 || rows[cur].Part != 0 || rows[cur+1].Part != 1 || rows[cur+1].Scale != 2 {
		t.Errorf("scaled block layout wrong: %+v / %+v", rows[cur], rows[cur+1])
	}
}

func TestWipeSplitsCurrentLine(t *testing.T) {
	var r Renderer
	f := testFrame(t, config.ModeFade, 25*time.Second)
	f.Theme.Wipe = true
	rows := r.Render(f)
	cur := rows[findRow(rows, "Second line")]
	var texts []string
	for _, s := range cur.Segs {
		if s.Text != "" {
			texts = append(texts, s.Text)
		}
	}
	if len(texts) != 2 || strings.Join(texts, "") != "Second line" {
		t.Errorf("wipe segments = %q", texts)
	}
}
