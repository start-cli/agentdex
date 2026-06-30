package tui

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// colGap separates columns in a rendered table.
const colGap = "  "

// Table is a simple column-aligned table: a header row over zero or more string
// rows. Columns are sized to their widest cell. A row shorter than the header is
// padded with blanks; a longer row is an authoring error and panics, since the
// column set is fixed by the caller.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable starts a table with the given column headers.
func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

// Append adds one row. It must not have more cells than there are headers.
func (t *Table) Append(cells ...string) {
	if len(cells) > len(t.headers) {
		panic(fmt.Sprintf("tui: row has %d cells, table has %d columns", len(cells), len(t.headers)))
	}
	t.rows = append(t.rows, cells)
}

// Render writes the table to w. The header is styled with Header; cells are
// written plain. Trailing padding is trimmed so each line ends at its last cell.
func (t *Table) Render(w io.Writer) {
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range t.rows {
		for i, c := range row {
			if n := utf8.RuneCountInString(c); n > widths[i] {
				widths[i] = n
			}
		}
	}

	t.writeRow(w, t.headers, widths, Header.Sprint)
	for _, row := range t.rows {
		t.writeRow(w, row, widths, fmt.Sprint)
	}
}

// writeRow renders one row, padding every cell but the last to its column width
// and styling each cell through style. The last populated cell is not padded, so
// lines carry no trailing whitespace.
func (t *Table) writeRow(w io.Writer, cells []string, widths []int, style func(...any) string) {
	last := len(cells) - 1
	var b strings.Builder
	for i := range t.headers {
		if i > 0 {
			b.WriteString(colGap)
		}
		var cell string
		if i < len(cells) {
			cell = cells[i]
		}
		if i == last {
			b.WriteString(style(cell))
			break
		}
		pad := widths[i] - utf8.RuneCountInString(cell)
		b.WriteString(style(cell))
		if pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	fmt.Fprintln(w, b.String())
}
