package output

import (
	"bytes"
	"testing"
)

// Field rows are the human `show` layout: labels padded to the widest label
// with a two-space gutter, values verbatim with no trailing padding. Pinned
// because agents grep this output too.
func TestRenderFields(t *testing.T) {
	tests := []struct {
		name string
		rows [][2]string
		want string
	}{
		{
			name: "labels align to the widest label",
			rows: [][2]string{
				{"NAME", "Sherlock"},
				{"DOWNLOADS", "42"},
			},
			want: "NAME       Sherlock\n" +
				"DOWNLOADS  42\n",
		},
		{
			name: "no rows renders nothing",
			rows: nil,
			want: "",
		},
		{
			name: "values carry no trailing padding",
			rows: [][2]string{
				{"A", "x"},
				{"LONGLABEL", "y"},
			},
			want: "A          x\n" +
				"LONGLABEL  y\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			RenderFields(&buf, tt.rows)
			if buf.String() != tt.want {
				t.Errorf("RenderFields output:\n%q\nwant:\n%q", buf.String(), tt.want)
			}
		})
	}
}
