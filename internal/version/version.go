// Package version holds build metadata. The variables are overridden at
// release time via -ldflags; the zero values identify a development build.
package version

import "fmt"

// Build metadata, set via ldflags at release time, e.g.:
//
//	-ldflags "-X github.com/positronick/cli/internal/version.Version=v0.1.0"
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single human-readable version line,
// e.g. "positronick dev (none, unknown)".
func String() string {
	return fmt.Sprintf("positronick %s (%s, %s)", Version, Commit, Date)
}
