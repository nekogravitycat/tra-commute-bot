package render

import "strings"

type align int

const (
	alignLeft align = iota
	alignRight
)

// gap is the blank run between columns. Two cells is the narrowest gap that
// still reads as a column break without rules to separate them.
const gap = "  "

// table lays out the comparison grid.
//
// It uses no box-drawing rules. Telegram's mobile clients wrap a <pre> block
// at roughly forty monospace characters, and a ruled table of these columns
// landed right on that limit — at which point the wrap destroys the alignment
// that was the entire reason for the monospace block. Dropping the rules and
// their inner padding brings the same content comfortably under it.
//
// All content is ASCII; see width.go for why that is a requirement rather
// than a preference.
//
// Column widths are measured from the content rather than fixed, so a longer
// train number or a three-digit delay cannot break the layout.
type table struct {
	headers []string
	aligns  []align
	rows    [][]string
}

func (t *table) addRow(cells ...string) { t.rows = append(t.rows, cells) }

func (t *table) render() string {
	if len(t.headers) == 0 {
		return ""
	}

	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = cellWidth(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && cellWidth(cell) > widths[i] {
				widths[i] = cellWidth(cell)
			}
		}
	}

	var b strings.Builder
	b.WriteString(t.line(t.headers, widths))
	for _, row := range t.rows {
		b.WriteString(t.line(row, widths))
	}
	return b.String()
}

func (t *table) line(cells []string, widths []int) string {
	var b strings.Builder

	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		if i > 0 {
			b.WriteString(gap)
		}

		// A heading takes its column's own alignment, so it sits directly
		// over the values rather than drifting to the far side of them.
		a := alignLeft
		if i < len(t.aligns) {
			a = t.aligns[i]
		}
		if a == alignRight {
			b.WriteString(padLeft(cell, w))
		} else {
			b.WriteString(padRight(cell, w))
		}
	}

	// Trailing blanks serve no purpose and only bring the line closer to the
	// wrap threshold.
	return strings.TrimRight(b.String(), " ") + "\n"
}
