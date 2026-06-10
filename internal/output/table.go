package output

import (
	"fmt"
	"io"
)

// RenderTable writes headers and rows as plain aligned columns: each column
// is padded to its widest cell, columns are separated by a two-space gutter,
// and the last cell of each line carries no trailing padding. No color, no
// external dependencies.
func RenderTable(w io.Writer, headers []string, rows [][]string) {
	cols := len(headers)
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}

	widths := make([]int, cols)
	lines := make([][]string, 0, len(rows)+1)
	lines = append(lines, headers)
	lines = append(lines, rows...)
	for _, line := range lines {
		for i, cell := range line {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	for _, line := range lines {
		for i, cell := range line {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			if i == len(line)-1 {
				fmt.Fprint(w, cell)
			} else {
				fmt.Fprintf(w, "%-*s", widths[i], cell)
			}
		}
		fmt.Fprintln(w)
	}
}
