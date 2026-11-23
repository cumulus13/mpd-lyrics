package lrc

import (
	"testing"
	"time"
)

func TestParseBasic(t *testing.T) {
	src := "[ar:Artist]\n[ti:Title]\n[00:12.50]Hello\n[00:05.5]Intro\n[01:02]World\n"
	l := Parse(src)
	if !l.Synced || len(l.Lines) != 3 {
		t.Fatalf("synced=%v lines=%d", l.Synced, len(l.Lines))
	}
	if l.Meta["ar"] != "Artist" || l.Meta["ti"] != "Title" {
		t.Fatalf("meta = %v", l.Meta)
	}
	want := []struct {
		t time.Duration
		s string
	}{
		{5500 * time.Millisecond, "Intro"},
		{12500 * time.Millisecond, "Hello"},
		{62 * time.Second, "World"},
	}
	for i, w := range want {
		if l.Lines[i].Time != w.t || l.Lines[i].Text != w.s {
			t.Errorf("line %d = %v %q, want %v %q", i, l.Lines[i].Time, l.Lines[i].Text, w.t, w.s)
		}
	}
}

func TestParseMultiStampAndWordTags(t *testing.T) {
	l := Parse("[00:10.00][00:40.00]<00:10.00>la <00:10.50>la\n")
	if len(l.Lines) != 2 {
		t.Fatalf("lines = %d", len(l.Lines))
	}
	for _, ln := range l.Lines {
		if ln.Text != "la la" {
			t.Errorf("text = %q", ln.Text)
		}
	}
}

func TestParseOffsetTag(t *testing.T) {
	l := Parse("[offset:+500]\n[00:10.00]x\n[00:00.20]y\n")
	if l.Lines[0].Time != 0 {
		t.Errorf("clamp failed: %v", l.Lines[0].Time)
	}
	if l.Lines[1].Time != 9500*time.Millisecond {
		t.Errorf("offset failed: %v", l.Lines[1].Time)
	}
}

func TestFractionDigits(t *testing.T) {
	cases := map[string]time.Duration{
		"[00:01.5]a":   1500 * time.Millisecond,
		"[00:01.05]a":  1050 * time.Millisecond,
		"[00:01.500]a": 1500 * time.Millisecond,
		"[00:01:25]a":  1250 * time.Millisecond,
	}
	for in, want := range cases {
		if got := Parse(in).Lines[0].Time; got != want {
			t.Errorf("%s: got %v want %v", in, got, want)
		}
	}
}

func TestIndexAtAndEnd(t *testing.T) {
	l := Parse("[00:10.00]a\n[00:20.00]b\n[00:30.00]\n")
	if got := l.IndexAt(5 * time.Second); got != -1 {
		t.Errorf("before first = %d", got)
	}
	if got := l.IndexAt(10 * time.Second); got != 0 {
		t.Errorf("at first = %d", got)
	}
	if got := l.IndexAt(25 * time.Second); got != 1 {
		t.Errorf("mid = %d", got)
	}
	if got := l.IndexAt(time.Hour); got != 2 {
		t.Errorf("after = %d", got)
	}
	if got := l.End(0, 0); got != 20*time.Second {
		t.Errorf("end0 = %v", got)
	}
	if got := l.End(2, 100*time.Second); got != 100*time.Second {
		t.Errorf("end last = %v", got)
	}
	if got := l.End(2, 0); got != 36*time.Second {
		t.Errorf("end last fallback = %v", got)
	}
	if l.Lines[2].Display() != "♪" {
		t.Errorf("blank display = %q", l.Lines[2].Display())
	}
}

func TestEstimate(t *testing.T) {
	p := FromPlain("\none\n\nthree three three\nfour\n\n")
	if len(p.Lines) != 4 {
		t.Fatalf("plain lines = %d", len(p.Lines))
	}
	e := Estimate(p, 200*time.Second)
	if !e.Estimated || len(e.Lines) != 4 {
		t.Fatalf("estimated=%v lines=%d", e.Estimated, len(e.Lines))
	}
	for i := 1; i < len(e.Lines); i++ {
		if e.Lines[i].Time <= e.Lines[i-1].Time {
			t.Errorf("times not increasing at %d", i)
		}
	}
	if last := e.Lines[len(e.Lines)-1].Time; last >= 200*time.Second {
		t.Errorf("last line beyond duration: %v", last)
	}
}
