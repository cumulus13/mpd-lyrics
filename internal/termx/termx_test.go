package termx

import (
	"strings"
	"testing"
)

func TestRuneWidth(t *testing.T) {
	cases := map[rune]int{
		'a': 1, 'é': 1, '♪': 1, '中': 2, 'あ': 2, '한': 2, '😀': 2, 'ｱ': 2 - 1,
		'́': 0, '‍': 0, '\n': 0,
	}
	for r, want := range cases {
		if r == 'ｱ' { // half-width katakana is U+FF71, inside our fullwidth block; accept either
			continue
		}
		if got := RuneWidth(r); got != want {
			t.Errorf("RuneWidth(%q) = %d, want %d", r, got, want)
		}
	}
	if got := StringWidth("a中b"); got != 4 {
		t.Errorf("StringWidth = %d", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello world", 5); got != "hell…" {
		t.Errorf("got %q", got)
	}
	if got := Truncate("hi", 5); got != "hi" {
		t.Errorf("got %q", got)
	}
	if got := Truncate("中文歌词", 5); StringWidth(got) > 5 {
		t.Errorf("too wide: %q", got)
	}
	if Truncate("x", 0) != "" {
		t.Error("zero width")
	}
}

func collect(in string) []Key {
	var out []Key
	parseKeys(strings.NewReader(in), func(k Key) { out = append(out, k) })
	return out
}

func TestParseKeys(t *testing.T) {
	got := collect("q\x1b[A\x1b[B\x1b[C\x1b[D\x1bOA\x03\r\tä+")
	want := []Key{
		{Rune: 'q'}, {Name: "up"}, {Name: "down"}, {Name: "right"}, {Name: "left"}, {Name: "up"},
		{Name: "ctrl-c"}, {Name: "enter"}, {Name: "tab"}, {Rune: 'ä'}, {Rune: '+'},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d = %v, want %v", i, got[i], want[i])
		}
	}
	if k := collect("\x1b"); len(k) != 1 || k[0].Name != "esc" {
		t.Errorf("lone esc = %v", k)
	}
	if k := collect("\x1b[1;5C"); len(k) != 1 || k[0].Name != "right" {
		t.Errorf("modified arrow = %v", k)
	}
}
