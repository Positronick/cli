package output

import (
	"os"

	"golang.org/x/term"
)

// isTerminal is a seam over term.IsTerminal so tests can simulate a TTY.
var isTerminal = func(fd int) bool { return term.IsTerminal(fd) }

// Mode describes the execution environment and the resulting rendering
// preferences for a single CLI invocation.
type Mode struct {
	// TTY is true when stdout is a terminal.
	TTY bool
	// CI is true when running under a CI system (CI or GITHUB_ACTIONS set).
	CI bool
	// Agent identifies a detected coding agent ("claude-code", "gemini-cli",
	// "cursor") or is empty when none is detected.
	Agent string
	// Color is true when ANSI color output is appropriate.
	Color bool
	// JSON is true when the user requested machine-readable JSON output.
	JSON bool
}

// DetectMode inspects flags and the environment and returns the output Mode
// for this invocation.
func DetectMode(jsonFlag, noColorFlag bool) Mode {
	m := Mode{
		TTY:  isTerminal(int(os.Stdout.Fd())),
		CI:   os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "",
		JSON: jsonFlag,
	}
	switch {
	case os.Getenv("CLAUDECODE") != "":
		m.Agent = "claude-code"
	case os.Getenv("GEMINI_CLI") != "":
		m.Agent = "gemini-cli"
	case os.Getenv("CURSOR_EDITOR") != "":
		m.Agent = "cursor"
	}
	m.Color = m.TTY && !m.CI && m.Agent == "" && !noColorFlag && os.Getenv("NO_COLOR") == ""
	return m
}

// Interactive reports whether it is appropriate to prompt the user:
// stdout is a TTY and we are not running under CI or a coding agent.
func (m Mode) Interactive() bool {
	return m.TTY && !m.CI && m.Agent == ""
}
