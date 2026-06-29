package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// docFlag describes one flag in the agent-docs JSON contract.
type docFlag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand"`
	Usage     string `json:"usage"`
	Default   string `json:"default"`
}

// docCommand describes one command in the agent-docs JSON contract.
type docCommand struct {
	Path        string    `json:"path"`
	Use         string    `json:"use"`
	Description string    `json:"description"`
	Flags       []docFlag `json:"flags"`
}

// agentDocs is the `agent-docs --json` contract:
// {"commands":[...],"exitCodes":{...}}.
type agentDocs struct {
	Commands  []docCommand      `json:"commands"`
	ExitCodes map[string]string `json:"exitCodes"`
}

// exitCodeMeanings maps each process exit code to its meaning, mirroring the
// constants in internal/output (the public contract).
var exitCodeMeanings = map[string]string{
	"0": "ok",
	"1": "error",
	"2": "cancelled",
	"3": "not found",
	"4": "auth required",
}

const agentDocsIntro = "`positronick` is the command-line client for positronick.com. It discovers agent " +
	"capabilities: souls (installable SOUL.md personality files), a registry of official " +
	"tooling (harnesses, CLIs, MCP servers, agents, skills, plugins, loops), and a `research` " +
	"feed of what's new (articles, releases, links) so agents avoid stale knowledge. It is built to be " +
	"driven by coding agents — pass `--json` to any command for stable machine-readable JSON on " +
	"stdout, read progress and errors from stderr, and branch on the exit code. Read commands " +
	"never prompt. Hidden admin commands (create/update for souls and listings, plus " +
	"create/list for profiles) exist and appear in help and in these docs after logging in " +
	"with an admin account."

func newAgentDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent-docs",
		Short: "Print the agent-facing manual for this CLI",
		Long: "Print a machine-friendly manual of every command, flag, exit code and output " +
			"contract — the CLI's self-description for coding agents.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			docs := collectDocs(cmd.Root())
			if p.Mode.JSON {
				return p.EmitJSON(docs)
			}
			p.Human("%s", renderAgentDocs(docs))
			return nil
		},
	}
}

// collectDocs walks the command tree depth-first in cobra's sorted order,
// skipping hidden commands and the help command.
func collectDocs(root *cobra.Command) agentDocs {
	var commands []docCommand
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden || c.Name() == "help" {
			return
		}
		commands = append(commands, docCommandFor(c))
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
	return agentDocs{Commands: commands, ExitCodes: exitCodeMeanings}
}

// docCommandFor captures one command's path, usage line, description and its
// own (non-inherited) flags. The auto-generated help flag is noise and is
// skipped.
func docCommandFor(c *cobra.Command) docCommand {
	flags := []docFlag{}
	c.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Hidden {
			return
		}
		flags = append(flags, docFlag{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Usage:     f.Usage,
			Default:   f.DefValue,
		})
	})
	return docCommand{
		Path:        c.CommandPath(),
		Use:         c.UseLine(),
		Description: c.Short,
		Flags:       flags,
	}
}

// renderAgentDocs renders the docs as an llms.txt-style markdown manual: H1,
// intro, exit-code table, the error-envelope shape, then one H2 per command.
func renderAgentDocs(d agentDocs) string {
	var b strings.Builder
	b.WriteString("# positronick — agent manual\n\n")
	b.WriteString(agentDocsIntro + "\n\n")

	b.WriteString("Exit codes:\n\n")
	b.WriteString("| Code | Meaning |\n| ---- | ------- |\n")
	for _, code := range []string{"0", "1", "2", "3", "4"} {
		fmt.Fprintf(&b, "| %s | %s |\n", code, d.ExitCodes[code])
	}

	b.WriteString("\nOn failure with `--json`, stderr carries exactly one error-envelope line:\n\n")
	b.WriteString("```json\n" +
		`{"error":{"code":"<code>","message":"<message>","hint":"<optional hint>"}}` +
		"\n```\n\n")
	b.WriteString("Error codes: `error`, `cancelled`, `not_found`, `auth_required`. " +
		"`hint` is omitted when empty.\n")

	for _, c := range d.Commands {
		fmt.Fprintf(&b, "\n## %s\n\nUsage: `%s`\n", c.Path, c.Use)
		if c.Description != "" {
			fmt.Fprintf(&b, "\n%s\n", c.Description)
		}
		if len(c.Flags) > 0 {
			b.WriteString("\nFlags:\n\n")
			for _, f := range c.Flags {
				name := "--" + f.Name
				if f.Shorthand != "" {
					name = "-" + f.Shorthand + ", " + name
				}
				fmt.Fprintf(&b, "- `%s` (default `%s`): %s\n", name, f.Default, f.Usage)
			}
		}
	}
	return b.String()
}
