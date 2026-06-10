// Package cli wires the cobra command tree. Commands return errors up to
// Execute, which renders them exactly once through internal/output and maps
// them to the process exit code.
package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// NewRootCmd builds the positronick root command with its persistent flags
// and all registered subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "positronick",
		Short:         "Discover and install agent capabilities from positronick.com",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	flags := root.PersistentFlags()
	flags.Bool("json", false, "emit machine-readable JSON on stdout")
	flags.String("base-url", "", "override the positronick.com API base URL")
	flags.Bool("no-color", false, "disable color output")
	flags.Bool("quiet", false, "suppress progress output on stderr")
	flags.Bool("yes", false, "assume yes for confirmation prompts")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newSoulCmd())
	for _, listingType := range api.ListingTypes {
		root.AddCommand(newListingNounCmd(listingType))
	}
	root.AddCommand(newLoginCmd(), newLogoutCmd(), newAuthCmd())
	root.AddCommand(newAgentDocsCmd())
	attachInstallCommands(root)
	registerAdminCommands(root) // hidden write commands; revealed for cached admins
	attachMCPServeCommand(root)

	return root
}

// Execute runs the CLI and returns the process exit code. SIGINT/SIGTERM
// cancel the command context, surfacing as ExitCancelled; any error is
// rendered exactly once via output.RenderError.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := NewRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		return output.RenderError(os.Stderr, err, wasJSONRequested(root, os.Args[1:]))
	}
	return output.ExitOK
}

// wasJSONRequested reports whether the user asked for JSON output. The parsed
// persistent flag is authoritative; when cobra failed before flag parsing
// (e.g. unknown command), it falls back to scanning the raw args so agents
// still receive the JSON error envelope they asked for.
func wasJSONRequested(root *cobra.Command, args []string) bool {
	if f := root.PersistentFlags().Lookup("json"); f != nil && f.Changed {
		return f.Value.String() == "true"
	}
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--json" || a == "--json=true" {
			return true
		}
	}
	return false
}
