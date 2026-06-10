package output

import (
	"fmt"
	"io"
)

// RenderFields writes label/value rows as two aligned columns — labels padded
// to the widest label, a two-space gutter, values verbatim with no trailing
// padding. It is the human layout for `show` commands.
func RenderFields(w io.Writer, rows [][2]string) {
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	for _, row := range rows {
		fmt.Fprintf(w, "%-*s  %s\n", width, row[0], row[1])
	}
}
