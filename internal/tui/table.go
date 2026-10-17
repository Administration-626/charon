package tui

import (
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// The bindings and model-library screens render as tables: one line per row,
// cells laid out in aligned columns. A row's cells travel on the item
// (item.cells) and the delegate measures them across the whole list on every
// render, so columns stay aligned as rows are added, removed, or the terminal
// is resized — nothing is pre-padded into the item text.

// bindingTableHeader / modelTableHeader label the columns of each screen.
var (
	bindingTableHeader = []string{"name", "endpoint", "model", "more"}
	modelTableHeader   = []string{"model", "endpoint", "context", "source"}
)

// tableGap separates two columns; two spaces keep columns scannable without
// drawing a border.
const tableGap = "  "

// tableMinColumn is the floor a column shrinks to before the row itself is
// truncated.
const tableMinColumn = 3

// tableHeaderStyle / tableNoteStyle render the column labels and the drift
// annotation under a binding row: muted, no fill, one leading space so they
// line up with the data rows.
var (
	tableHeaderStyle = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 0, 0, 1)
	tableNoteStyle   = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 0, 0, 1)
)

// tableColumnWidths measures every table row in the list and returns one width
// per column: the widest cell, floored at the header label so headers survive,
// then shrunk until the whole row fits the list's width. The widest column
// gives up space first, which in practice means a long endpoint before names.
func tableColumnWidths(m list.Model) []int {
	var header []string
	var rows [][]string
	cols := 0
	for _, raw := range m.Items() {
		it, ok := raw.(item)
		if !ok || it.cells == nil {
			continue
		}
		if it.value == hdrSentinel {
			header = it.cells
		} else {
			rows = append(rows, it.cells)
		}
		if len(it.cells) > cols {
			cols = len(it.cells)
		}
	}
	widths := make([]int, cols)
	for i := range widths {
		if i < len(header) {
			widths[i] = lipgloss.Width(header[i])
		}
	}
	for _, row := range rows {
		for i, cell := range row {
			if w := lipgloss.Width(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	// One leading pad plus a right margin keeps every line inside the viewport.
	avail := m.Width() - 3
	for tableRowWidth(widths) > avail {
		widest := -1
		for i, w := range widths {
			if w > tableMinColumn && (widest < 0 || w > widths[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
	}
	return widths
}

// tableRowWidth is the display width of a laid-out row: cells plus the gaps
// between them.
func tableRowWidth(widths []int) int {
	total := (len(widths) - 1) * len(tableGap)
	for _, w := range widths {
		total += w
	}
	return total
}

// joinCells lays cells out in columns of the given widths, padding each cell
// with spaces and truncating any that overflow.
func joinCells(cells []string, widths []int) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		w := 0
		if i < len(widths) {
			w = widths[i]
		}
		parts[i] = fitCell(cell, w)
	}
	return strings.Join(parts, tableGap)
}

// fitCell pads s to width, or truncates it with an ellipsis when it overflows.
func fitCell(s string, width int) string {
	if width <= 0 {
		return s
	}
	if pad := width - lipgloss.Width(s); pad >= 0 {
		return s + strings.Repeat(" ", pad)
	}
	return truncateCell(s, width)
}

// truncateCell keeps the first width-1 display columns of s and appends an
// ellipsis, so an overlong endpoint still reads as one table cell.
func truncateCell(s string, width int) string {
	if width <= 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

// renderTableHeader draws the column labels in muted text over the same
// columns the data rows use.
func renderTableHeader(w io.Writer, m list.Model, it item) {
	_, _ = io.WriteString(w, tableHeaderStyle.Render(joinCells(it.cells, tableColumnWidths(m))))
}

// renderTableRow draws one data row: aligned columns, the whole row filled
// with the selection color when the cursor is on it, and — when the cursor is
// elsewhere — the active binding's name in the brand color so "what's active"
// and "what's under the cursor" stay two distinct signals.
func (d charonDelegate) renderTableRow(w io.Writer, m list.Model, index int, it item) {
	widths := tableColumnWidths(m)
	if index == m.Index() {
		_, _ = io.WriteString(w, d.Styles.SelectedTitle.Render(joinCells(it.cells, widths)))
		return
	}
	if it.active && len(it.cells) > 0 {
		name := lipgloss.NewStyle().Foreground(colorBrand).Bold(true).Render(fitCell(it.cells[0], widths[0]))
		row := name
		if len(it.cells) > 1 {
			row += tableGap + joinCells(it.cells[1:], widths[1:])
		}
		_, _ = io.WriteString(w, d.Styles.NormalTitle.Render(row))
		return
	}
	_, _ = io.WriteString(w, d.Styles.NormalTitle.Render(joinCells(it.cells, widths)))
}
