package edit

import "testing"

func TestUnquote(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", false},
		{"plain", "", false},
		{`"a"`, "a", true},
		{`'a'`, "a", true},
		{`"a\"b"`, `a"b`, true},
		{`"a"   # note`, "a", true},
		{`'a' # note`, "a", true},
		{`"open`, "open", true},
		{`""`, "", true},
	}
	for _, c := range cases {
		got, ok := unquote(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("unquote(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestTomlValue(t *testing.T) {
	cases := map[string]string{
		`"a" # note`: "a",
		`'a'`:        "a",
		"1 # note":   "1",
		"true":       "true",
		`""`:         "",
		"plain":      "plain",
	}
	for in, want := range cases {
		if got := tomlValue(in); got != want {
			t.Errorf("tomlValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"s", "s"},
		{true, "true"},
		{7, "7"},
		{1.5, "1.5"},
	}
	for _, c := range cases {
		if got := toString(c.in); got != c.want {
			t.Errorf("toString(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitLinesEmpty(t *testing.T) {
	lines := splitLines("")
	if len(lines) != 1 || lines[0] != "" {
		t.Errorf("splitLines(\"\") = %#v, want one empty line", lines)
	}
}
