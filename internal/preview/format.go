package preview

import (
	"fmt"
	"strings"
)

// contextLines is how many unchanged lines a hunk shows around each change.
const contextLines = 3

// maxDiffLines bounds the LCS: past this a file renders as whole-file
// replacement, which keeps the preview cheap on the rare huge config.
const maxDiffLines = 5000

type op struct {
	kind byte // ' ', '-', '+'
	line string
}

// diffLines computes a line diff of a and b by longest common subsequence.
func diffLines(a, b []string) []op {
	n, m := len(a), len(b)
	// lcs[i][j]: length of the LCS of a[i:], b[j:]
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	return ops
}

// splitLines keeps a trailing newline as a final empty element, so the last
// line renders with or without a newline exactly as the file has it.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Render is d as a unified diff, or "" when it would not change.
func (d FileDiff) Render() string {
	if !d.Changed() {
		return ""
	}
	var b strings.Builder
	switch {
	case d.Before == nil:
		fmt.Fprintf(&b, "(new file)\n")
		for _, l := range splitLines(d.After) {
			b.WriteString("+")
			b.WriteString(l)
			b.WriteString("\n")
		}
		return b.String()
	case d.After == nil:
		fmt.Fprintf(&b, "(file removed)\n")
		for _, l := range splitLines(d.Before) {
			b.WriteString("-")
			b.WriteString(l)
			b.WriteString("\n")
		}
		return b.String()
	}
	a, c := splitLines(d.Before), splitLines(d.After)
	if len(a) > maxDiffLines || len(c) > maxDiffLines {
		fmt.Fprintf(&b, "(file replaced; %d → %d lines)\n", len(a), len(c))
		return b.String()
	}
	ops := diffLines(a, c)

	// group the ops into hunks: a change starts a hunk, changes fewer than
	// 2*context+1 apart share one
	type hunk struct {
		start, end   int // ops range [start, end)
		aFrom, bFrom int // 1-based line numbers the hunk starts at
	}
	var hunks []hunk
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := max(0, i-contextLines)
		end := i + 1
		// changes closer than 2*context apart share one hunk
		for {
			next := end
			for next < len(ops) && ops[next].kind == ' ' {
				next++
			}
			if next < len(ops) && next-end <= 2*contextLines {
				end = next + 1
				continue
			}
			break
		}
		end = min(len(ops), end+contextLines) // trailing context
		h := hunk{start: start, end: end, aFrom: 1, bFrom: 1}
		for k := 0; k < start; k++ {
			if ops[k].kind == ' ' || ops[k].kind == '-' {
				h.aFrom++
			}
			if ops[k].kind == ' ' || ops[k].kind == '+' {
				h.bFrom++
			}
		}
		hunks = append(hunks, h)
		i = end
	}
	for _, h := range hunks {
		aN, bN := 0, 0
		for k := h.start; k < h.end; k++ {
			switch ops[k].kind {
			case ' ', '-':
				aN++
			}
			switch ops[k].kind {
			case ' ', '+':
				bN++
			}
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.aFrom, aN, h.bFrom, bN)
		for k := h.start; k < h.end; k++ {
			b.WriteByte(ops[k].kind)
			b.WriteString(ops[k].line)
			b.WriteString("\n")
		}
	}
	return b.String()
}
