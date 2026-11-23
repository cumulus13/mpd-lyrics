// Package colorx parses colours and renders them as ANSI escape parameters at
// the best fidelity the terminal supports.
package colorx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RGB is an 8-bit-per-channel colour.
type RGB struct{ R, G, B uint8 }

var named = map[string]RGB{
	"black":   {0, 0, 0},
	"red":     {231, 76, 60},
	"green":   {46, 204, 113},
	"yellow":  {241, 196, 15},
	"blue":    {52, 152, 219},
	"magenta": {155, 89, 182},
	"cyan":    {26, 188, 156},
	"white":   {236, 240, 241},
	"gray":    {127, 140, 141},
	"grey":    {127, 140, 141},
	"orange":  {230, 126, 34},
	"pink":    {253, 121, 168},
	"purple":  {155, 89, 182},
	"lime":    {0, 255, 0},
}

// Parse accepts a colour name ("green"), "#rgb", "#rrggbb" or "rgb(r,g,b)".
func Parse(s string) (RGB, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return RGB{}, fmt.Errorf("empty colour")
	}
	if c, ok := named[s]; ok {
		return c, nil
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 {
			return RGB{}, fmt.Errorf("invalid hex colour %q", s)
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return RGB{}, fmt.Errorf("invalid hex colour %q", s)
		}
		return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(s[4:len(s)-1], ",")
		if len(parts) == 3 {
			var out [3]uint8
			for i, p := range parts {
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || n < 0 || n > 255 {
					return RGB{}, fmt.Errorf("invalid rgb() colour %q", s)
				}
				out[i] = uint8(n)
			}
			return RGB{out[0], out[1], out[2]}, nil
		}
	}
	return RGB{}, fmt.Errorf("unknown colour %q (use a name, #rrggbb or rgb(r,g,b))", s)
}

// Hex returns the #rrggbb form.
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// Lerp blends a towards b by t in [0,1].
func Lerp(a, b RGB, t float64) RGB {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t))
	}
	return RGB{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B)}
}

// Profile is the colour capability of the output terminal.
type Profile int

const (
	None Profile = iota
	ANSI16
	ANSI256
	TrueColor
)

// ParseProfile maps a config string to a Profile. "auto" yields ok=false.
func ParseProfile(s string) (p Profile, ok bool, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return 0, false, nil
	case "truecolor", "24bit", "24-bit":
		return TrueColor, true, nil
	case "256", "ansi256":
		return ANSI256, true, nil
	case "16", "ansi16":
		return ANSI16, true, nil
	case "none", "off", "mono":
		return None, true, nil
	}
	return 0, false, fmt.Errorf("unknown color_mode %q (auto|truecolor|256|16|none)", s)
}

// Detect inspects the environment. getenv is injected for tests.
func Detect(getenv func(string) string, isWindows bool) Profile {
	if getenv("NO_COLOR") != "" {
		return None
	}
	term := strings.ToLower(getenv("TERM"))
	if term == "dumb" {
		return None
	}
	ct := strings.ToLower(getenv("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return TrueColor
	}
	if getenv("WT_SESSION") != "" || getenv("KITTY_WINDOW_ID") != "" || getenv("WEZTERM_EXECUTABLE") != "" {
		return TrueColor
	}
	if strings.Contains(term, "truecolor") || strings.Contains(term, "direct") {
		return TrueColor
	}
	if strings.Contains(term, "256") {
		return ANSI256
	}
	if isWindows {
		return TrueColor
	}
	if term == "" {
		return ANSI16
	}
	return ANSI16
}

// FG returns the SGR parameter string selecting c as foreground, or "".
func (p Profile) FG(c RGB) string {
	switch p {
	case TrueColor:
		return fmt.Sprintf("38;2;%d;%d;%d", c.R, c.G, c.B)
	case ANSI256:
		return fmt.Sprintf("38;5;%d", to256(c))
	case ANSI16:
		return strconv.Itoa(to16(c))
	}
	return ""
}

func to256(c RGB) int {
	// 6x6x6 cube candidate
	lv := func(v uint8) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return int((int(v) - 35) / 40)
	}
	cr, cg, cb := lv(c.R), lv(c.G), lv(c.B)
	steps := [6]int{0, 95, 135, 175, 215, 255}
	cube := 16 + 36*cr + 6*cg + cb
	cd := dist2(c, RGB{uint8(steps[cr]), uint8(steps[cg]), uint8(steps[cb])})
	// grayscale ramp candidate
	avg := (int(c.R) + int(c.G) + int(c.B)) / 3
	gi := (avg - 8 + 5) / 10
	if gi < 0 {
		gi = 0
	}
	if gi > 23 {
		gi = 23
	}
	gv := uint8(8 + 10*gi)
	gd := dist2(c, RGB{gv, gv, gv})
	if gd < cd {
		return 232 + gi
	}
	return cube
}

var ansi16 = [16]RGB{
	{0, 0, 0}, {205, 49, 49}, {13, 188, 121}, {229, 229, 16},
	{36, 114, 200}, {188, 63, 188}, {17, 168, 205}, {229, 229, 229},
	{102, 102, 102}, {241, 76, 76}, {35, 209, 139}, {245, 245, 67},
	{59, 142, 234}, {214, 112, 214}, {41, 184, 219}, {255, 255, 255},
}

func to16(c RGB) int {
	best, bd := 0, math.MaxInt
	for i, a := range ansi16 {
		if d := dist2(c, a); d < bd {
			best, bd = i, d
		}
	}
	if best < 8 {
		return 30 + best
	}
	return 90 + best - 8
}

func dist2(a, b RGB) int {
	dr, dg, db := int(a.R)-int(b.R), int(a.G)-int(b.G), int(a.B)-int(b.B)
	return dr*dr + dg*dg + db*db
}
