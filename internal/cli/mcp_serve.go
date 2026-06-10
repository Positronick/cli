package cli

import (
	"os"

	"github.com/positronick/cli/internal/mcpserver"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// stdinIsTerminal is a seam over term.IsTerminal so tests can fake `mcp
// serve` running on (or off) a terminal.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// attachMCPServeCommand adds the reserved `serve` verb to the existing mcp
// noun by lookup. There is no parse ambiguity with the slug-taking verbs
// (`mcp show <slug>` etc.): slugs only ever appear in argument position.
func attachMCPServeCommand(root *cobra.Command) {
	for _, c := range root.Commands() {
		if c.Name() == "mcp" {
			c.AddCommand(newMCPServeCmd())
		}
	}
}

const mcpServeLong = `Run this binary as an MCP (Model Context Protocol) server over stdio,
exposing positronick's capabilities as five tools: soul_search, soul_show,
soul_install, listing_search and listing_show. They wrap the same machinery
as the CLI commands — including the auth credential chain and the download
counter semantics of soul_install.

When stdin is a terminal the server does not start: setup instructions for
Claude Code, Cursor and generic stdio clients are printed instead. Pass
--stdio to force the stdio server anyway.`

func newMCPServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run an MCP stdio server exposing positronick's tools",
		Long:  mcpServeLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stdio, err := cmd.Flags().GetBool("stdio")
			if err != nil {
				return err
			}
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			// An interactive shell is never the MCP client: print setup
			// instructions instead of hijacking the terminal with JSON-RPC.
			if !stdio && stdinIsTerminal() {
				p.Human("%s", mcpserver.SetupInstructions())
				return nil
			}

			client, err := clientFor(cmd)
			if err != nil {
				return err
			}
			cwd, home, err := resolveCwdHome()
			if err != nil {
				return err
			}
			return mcpserver.Run(cmd.Context(), mcpserver.Options{
				Client:  client,
				Cwd:     cwd,
				Home:    home,
				Suggest: closestSlug,
			})
		},
	}
	cmd.Flags().Bool("stdio", false, "run the stdio server even when stdin is a terminal")
	return cmd
}
