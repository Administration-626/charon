package edit

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TOML entries are located by the parser (toml.go), then edited without
// reformatting the rest of the file.

var (
	// tomlTable and tomlKV only read a TOML file its parser refuses; see
	// GetTOMLTop.
	tomlTable = regexp.MustCompile(`^\s*\[`)
	tomlKV    = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+|"[^"]*")\s*=\s*(.*?)\s*$`)
)

// unquote strips a leading quoted scalar (and anything after it, such as an
// inline comment); ok is false when v does not start with a quote.
func unquote(v string) (string, bool) {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return "", false
	}
	q := v[0]
	for i := 1; i < len(v); i++ {
		if q == '"' && v[i] == '\\' {
			i++
			continue
		}
		if v[i] == q {
			if q == '"' {
				if s, err := strconv.Unquote(v[:i+1]); err == nil {
					return s, true
				}
			}
			return v[1:i], true
		}
	}
	return v[1:], true
}

func tomlValue(v string) string {
	if s, ok := unquote(v); ok {
		return s
	}
	if i := strings.Index(v, "#"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	}
	return fmt.Sprint(v)
}

// splitLines splits on '\n' but keeps a trailing newline as an empty final
// element so joinLines can restore the file exactly.
func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

// joinLinesLike is joinLines for lines edited out of orig: when orig used
// \r\n for every line break, the lines added or replaced get \r\n too, so the
// file never ends up with mixed endings. Untouched lines already carry their
// \r, and a file that mixed endings is left as it was.
func joinLinesLike(lines []string, orig string) string {
	s := joinLines(lines)
	if n := strings.Count(orig, "\n"); n > 0 && strings.Count(orig, "\r\n") == n {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	return s
}

func joinLines(lines []string) string {
	s := strings.Join(lines, "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}
