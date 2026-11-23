package colorx

import "testing"

func TestParse(t *testing.T) {
	ok := map[string]RGB{
		"green":            named["green"],
		" YELLOW ":         named["yellow"],
		"#ff8000":          {255, 128, 0},
		"#f80":             {255, 136, 0},
		"rgb(1, 2, 3)":     {1, 2, 3},
		"#00ff00":          {0, 255, 0},
		"rgb(255,255,255)": {255, 255, 255},
	}
	for in, want := range ok {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "#12", "#gggggg", "rgb(1,2)", "rgb(300,0,0)", "nope"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestLerp(t *testing.T) {
	a, b := RGB{0, 0, 0}, RGB{200, 100, 50}
	if Lerp(a, b, 0) != a || Lerp(a, b, 1) != b {
		t.Fatal("endpoints")
	}
	if got := Lerp(a, b, 0.5); got != (RGB{100, 50, 25}) {
		t.Errorf("mid = %v", got)
	}
}

func TestDetect(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		name string
		env  map[string]string
		win  bool
		want Profile
	}{
		{"no_color", map[string]string{"NO_COLOR": "1", "COLORTERM": "truecolor"}, false, None},
		{"dumb", map[string]string{"TERM": "dumb"}, false, None},
		{"truecolor", map[string]string{"COLORTERM": "truecolor", "TERM": "xterm"}, false, TrueColor},
		{"256", map[string]string{"TERM": "xterm-256color"}, false, ANSI256},
		{"plain", map[string]string{"TERM": "xterm"}, false, ANSI16},
		{"windows", map[string]string{}, true, TrueColor},
		{"kitty", map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-kitty"}, false, TrueColor},
	}
	for _, c := range cases {
		if got := Detect(env(c.env), c.win); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestFG(t *testing.T) {
	c := RGB{46, 204, 113}
	if got := TrueColor.FG(c); got != "38;2;46;204;113" {
		t.Errorf("truecolor = %q", got)
	}
	if got := ANSI256.FG(RGB{255, 0, 0}); got != "38;5;196" {
		t.Errorf("256 red = %q", got)
	}
	if got := ANSI256.FG(RGB{128, 128, 128}); got[:5] != "38;5;" {
		t.Errorf("256 gray = %q", got)
	}
	if got := ANSI16.FG(RGB{0, 0, 0}); got != "30" {
		t.Errorf("16 black = %q", got)
	}
	if got := None.FG(c); got != "" {
		t.Errorf("none = %q", got)
	}
}

func TestParseProfile(t *testing.T) {
	if _, ok, err := ParseProfile("auto"); ok || err != nil {
		t.Error("auto")
	}
	if p, ok, _ := ParseProfile("256"); !ok || p != ANSI256 {
		t.Error("256")
	}
	if _, _, err := ParseProfile("bogus"); err == nil {
		t.Error("bogus")
	}
}
