package output

import (
	"bytes"
	"testing"
)

// NDJSON streams (e.g. `login --json`) need one compact JSON object per line
// — pretty-printing would break line-oriented consumers.
func TestEmitJSONLine(t *testing.T) {
	var out bytes.Buffer
	p := &Printer{Out: &out, Err: &bytes.Buffer{}, Mode: Mode{JSON: true}}
	if err := p.EmitJSONLine(map[string]any{"status": "pending", "expiresIn": 1800}); err != nil {
		t.Fatalf("EmitJSONLine: %v", err)
	}
	want := `{"expiresIn":1800,"status":"pending"}` + "\n"
	if out.String() != want {
		t.Errorf("out = %q, want %q (compact, newline-terminated)", out.String(), want)
	}
}

// Status lines are progress noise: machine consumers (--json) and users who
// asked for --quiet must never see them, while data on Out is untouched.
func TestStatusSuppression(t *testing.T) {
	tests := []struct {
		name       string
		mode       Mode
		quiet      bool
		wantStderr string
	}{
		{"human mode emits status", Mode{}, false, "fetching\n"},
		{"json mode suppresses status", Mode{JSON: true}, false, ""},
		{"quiet suppresses status", Mode{}, true, ""},
		{"quiet and json suppress status", Mode{JSON: true}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			p := &Printer{Out: &out, Err: &errBuf, Mode: tt.mode, Quiet: tt.quiet}
			p.Status("fetching\n")
			p.Human("data\n")
			if errBuf.String() != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", errBuf.String(), tt.wantStderr)
			}
			if out.String() != "data\n" {
				t.Errorf("stdout = %q, want %q (data must never be suppressed)", out.String(), "data\n")
			}
		})
	}
}
