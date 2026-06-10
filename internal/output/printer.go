package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Printer is the single writer for command output. Data goes to Out (stdout),
// progress goes to Err (stderr) so that piped/JSON consumers see clean data.
type Printer struct {
	Out  io.Writer
	Err  io.Writer
	Mode Mode
	// Quiet suppresses Status lines (--quiet); data on Out is never affected.
	Quiet bool
}

// New returns a Printer bound to os.Stdout and os.Stderr.
func New(mode Mode) *Printer {
	return &Printer{Out: os.Stdout, Err: os.Stderr, Mode: mode}
}

// EmitJSON writes v as 2-space-indented JSON followed by a newline to Out.
func (p *Printer) EmitJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(p.Out, string(b))
	return err
}

// Human writes formatted primary output to Out.
func (p *Printer) Human(format string, a ...any) {
	fmt.Fprintf(p.Out, format, a...)
}

// Status writes a formatted progress line to Err. It is suppressed in JSON
// mode — so machine consumers never see interleaved progress noise — and by
// Quiet (--quiet).
func (p *Printer) Status(format string, a ...any) {
	if p.Mode.JSON || p.Quiet {
		return
	}
	fmt.Fprintf(p.Err, format, a...)
}
