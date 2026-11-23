package config

import (
	"fmt"
	"strconv"
	"strings"
)

// parseTOML parses the small TOML subset used by mpd-lyrics:
//
//	[section] and [dotted.section] headers
//	key = "basic string" | 'literal string' | integer | float | true | false
//	# comments
//
// The result is a flat map keyed "section.key". Arrays, inline tables and
// multi-line strings are intentionally not supported.
func parseTOML(src string) (map[string]any, error) {
	out := map[string]any{}
	section := ""
	src = strings.TrimPrefix(src, "\ufeff")
	for i, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if strings.HasPrefix(line, "[[") {
				return nil, fmt.Errorf("line %d: arrays of tables are not supported", n)
			}
			end := strings.Index(line, "]")
			if end < 0 {
				return nil, fmt.Errorf("line %d: unterminated section header", n)
			}
			name := strings.TrimSpace(line[1:end])
			if !validKeyPath(name) {
				return nil, fmt.Errorf("line %d: invalid section name %q", n, name)
			}
			if rest := strings.TrimSpace(line[end+1:]); rest != "" && !strings.HasPrefix(rest, "#") {
				return nil, fmt.Errorf("line %d: unexpected text after section header", n)
			}
			section = name
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected key = value", n)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" || !validKeyPath(key) {
			return nil, fmt.Errorf("line %d: invalid key %q", n, key)
		}
		val, err := parseValue(strings.TrimSpace(line[eq+1:]))
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %v", n, key, err)
		}
		full := key
		if section != "" {
			full = section + "." + key
		}
		if _, dup := out[full]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", n, full)
		}
		out[full] = val
	}
	return out, nil
}

func validKeyPath(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return !strings.HasPrefix(s, ".") && !strings.HasSuffix(s, ".") && !strings.Contains(s, "..")
}

func parseValue(s string) (any, error) {
	if s == "" {
		return nil, fmt.Errorf("missing value")
	}
	switch s[0] {
	case '"':
		end := -1
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, fmt.Errorf("unterminated string")
		}
		if err := onlyComment(s[end+1:]); err != nil {
			return nil, err
		}
		str, err := strconv.Unquote(s[:end+1])
		if err != nil {
			return nil, fmt.Errorf("bad string escape")
		}
		return str, nil
	case '\'':
		end := strings.Index(s[1:], "'")
		if end < 0 {
			return nil, fmt.Errorf("unterminated string")
		}
		if err := onlyComment(s[end+2:]); err != nil {
			return nil, err
		}
		return s[1 : end+1], nil
	case '[', '{':
		return nil, fmt.Errorf("arrays and inline tables are not supported")
	}
	tok := s
	if i := strings.Index(tok, "#"); i >= 0 {
		tok = tok[:i]
	}
	tok = strings.TrimSpace(tok)
	switch tok {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	clean := strings.ReplaceAll(tok, "_", "")
	if n, err := strconv.ParseInt(clean, 0, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(clean, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("cannot parse value %q (strings must be quoted)", tok)
}

func onlyComment(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}
	return fmt.Errorf("unexpected text after value")
}
