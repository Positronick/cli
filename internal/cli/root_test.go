package cli

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := NewRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestRootHelpSucceeds(t *testing.T) {
	stdout, _, err := execute(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if !strings.Contains(stdout, "positronick") {
		t.Errorf("help output should mention the binary name, got %q", stdout)
	}
	if !strings.Contains(stdout, "--json") {
		t.Errorf("help output should list the persistent --json flag, got %q", stdout)
	}
}

// SilenceUsage/SilenceErrors keep failure output clean so Execute can render
// the error exactly once through internal/output — cobra must not also dump
// usage or the error text.
func TestUnknownCommandFailsSilently(t *testing.T) {
	stdout, stderr, err := execute(t, "definitely-not-a-command")
	if err == nil {
		t.Fatal("unknown command should return an error")
	}
	combined := stdout + stderr
	if strings.Contains(combined, "Usage:") {
		t.Errorf("usage must be silenced on error, got %q", combined)
	}
	if strings.Contains(combined, err.Error()) {
		t.Errorf("error must be silenced (rendered once by Execute), got %q", combined)
	}
}

// version --json is machine-readable: agents parse these exact four keys.
func TestVersionJSON(t *testing.T) {
	stdout, _, err := execute(t, "version", "--json")
	if err != nil {
		t.Fatalf("version --json returned error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("version --json output is not valid JSON: %v (%q)", err, stdout)
	}

	want := map[string]string{
		"version": "dev",
		"commit":  "none",
		"date":    "unknown",
		"go":      runtime.Version(),
	}
	if len(got) != len(want) {
		t.Errorf("version --json has %d keys, want %d (%q)", len(got), len(want), stdout)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("version --json %q = %q, want %q", k, got[k], v)
		}
	}
}

// When cobra fails before flag parsing (e.g. unknown command), --json never
// reaches the flag set — but an agent that asked for JSON must still get the
// JSON error envelope, so Execute falls back to scanning the raw args.
func TestWasJSONRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"no args", nil, false},
		{"unknown command with --json", []string{"nope", "--json"}, true},
		{"unknown command with --json=true", []string{"nope", "--json=true"}, true},
		{"unknown command without --json", []string{"nope"}, false},
		{"--json after terminator is positional", []string{"nope", "--", "--json"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewRootCmd()
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(tt.args)
			_ = root.Execute()
			if got := wasJSONRequested(root, tt.args); got != tt.want {
				t.Errorf("wasJSONRequested(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// And when flags do parse, the parsed value wins over the raw-args scan.
func TestWasJSONRequestedParsedFlagWins(t *testing.T) {
	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	args := []string{"version", "--json=false"}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("version --json=false returned error: %v", err)
	}
	if wasJSONRequested(root, args) {
		t.Error("parsed --json=false must report false even though --json appears in args")
	}
}

func TestVersionHuman(t *testing.T) {
	stdout, _, err := execute(t, "version")
	if err != nil {
		t.Fatalf("version returned error: %v", err)
	}
	want := "positronick dev (none, unknown)\n"
	if stdout != want {
		t.Errorf("version output = %q, want %q", stdout, want)
	}
}
