package output

import (
	"bytes"
	"testing"
)

// Table output is consumed by humans and by agents that grep stdout, so the
// exact layout (per-column max width, two-space gutter, no trailing padding)
// is pinned with golden strings.
func TestRenderTable(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		rows    [][]string
		want    string
	}{
		{
			name:    "columns align to widest cell with two-space gutter",
			headers: []string{"NAME", "TYPE", "DESCRIPTION"},
			rows: [][]string{
				{"sherlock", "soul", "Deductive reasoning"},
				{"poirot-long-name", "agent", "x"},
			},
			want: "NAME              TYPE   DESCRIPTION\n" +
				"sherlock          soul   Deductive reasoning\n" +
				"poirot-long-name  agent  x\n",
		},
		{
			name:    "no rows renders headers only",
			headers: []string{"NAME", "TYPE"},
			rows:    nil,
			want:    "NAME  TYPE\n",
		},
		{
			name:    "single column has no padding",
			headers: []string{"SLUG"},
			rows: [][]string{
				{"a"},
				{"much-longer-slug"},
			},
			want: "SLUG\na\nmuch-longer-slug\n",
		},
		{
			name:    "header wider than every cell sets the column width",
			headers: []string{"IDENTIFIER", "V"},
			rows: [][]string{
				{"ab", "1"},
			},
			want: "IDENTIFIER  V\n" +
				"ab          1\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			RenderTable(&buf, tt.headers, tt.rows)
			if buf.String() != tt.want {
				t.Errorf("RenderTable output:\n%q\nwant:\n%q", buf.String(), tt.want)
			}
		})
	}
}
