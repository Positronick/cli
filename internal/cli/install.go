package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/install"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// attachInstallCommands wires the install verbs onto the existing noun
// commands by lookup (souls and loops have bespoke installs; every other
// registry noun shares the install-command flow) and registers the
// root-level init and self commands.
func attachInstallCommands(root *cobra.Command) {
	for _, c := range root.Commands() {
		switch c.Name() {
		case "soul":
			c.AddCommand(newSoulInstallCmd())
		case "loop":
			c.AddCommand(newLoopInstallCmd())
		case "harness", "cli", "mcp", "agent", "skill", "plugin":
			c.AddCommand(newListingInstallCmd(c.Name()))
		}
	}
	root.AddCommand(newInitCmd(), newSelfCmd())
}

// installedSoul is the payload of the `soul install --json` contract.
type installedSoul struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Target  string `json:"target"`
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
}

// soulInstallResult is the `soul install --json` contract:
// {"installed":{"slug","name","version","target","path","bytes"}}.
type soulInstallResult struct {
	Installed installedSoul `json:"installed"`
}

// listingRef is the slim listing reference embedded in install results.
type listingRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// loopInstallResult is the `loop install --json` contract.
type loopInstallResult struct {
	Listing listingRef `json:"listing"`
	Kickoff string     `json:"kickoff"`
}

// listingInstallResult is the `<noun> install --json` contract. ExitCode is
// present only after --run actually executed the command.
type listingInstallResult struct {
	Listing    listingRef `json:"listing"`
	InstallCmd string     `json:"installCmd"`
	Executed   bool       `json:"executed"`
	ExitCode   *int       `json:"exitCode,omitempty"`
}

const soulInstallLong = `Fetch a soul's SOUL.md body verbatim and write it where the target framework
reads it:

  hermes    ~/.hermes/SOUL.md (the default)
  claude    ~/.claude/SOUL.md (--link also adds an @-import to ~/.claude/CLAUDE.md)
  cursor    ./.cursor/rules/soul.mdc (wrapped in mdc frontmatter)
  openclaw  ~/.openclaw/SOUL.md

Without --target the harness is detected from marker directories (.hermes,
.claude, .cursor, .openclaw) in the working directory, then your home
directory; hermes is the fallback. --path overrides everything and writes the
body verbatim to that exact file (the cursor wrapper applies only when
--target cursor is set explicitly).

This is the one command that counts as a download on positronick.com. An
existing file is only overwritten after a confirmation prompt (interactive)
or with --force.`

func newSoulInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <slug>",
		Short: "Install a soul's SOUL.md for your agent framework",
		Long:  soulInstallLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := cmd.Flags().GetString("target")
			if err != nil {
				return err
			}
			path, err := cmd.Flags().GetString("path")
			if err != nil {
				return err
			}
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			link, err := cmd.Flags().GetBool("link")
			if err != nil {
				return err
			}
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			client, err := clientFor(cmd)
			if err != nil {
				return err
			}

			explicitTarget := cmd.Flags().Changed("target")
			cwd, home, err := resolveCwdHome()
			if err != nil {
				return err
			}
			if !explicitTarget {
				if target = install.DetectHarness(cwd, home); target == "" {
					target = "hermes"
				}
			} else if _, err := install.TargetPath(target, cwd, home); err != nil {
				return output.Errorf("%s", err)
			}
			if link && path != "" {
				return output.Errorf("--link cannot be combined with --path")
			}
			if link && target != "claude" {
				return output.ErrorWithHint("--link only applies to the claude target",
					"re-run with --target claude")
			}

			spec := soulInstallSpec{
				slug:          args[0],
				target:        target,
				reportTarget:  target,
				path:          path,
				cwd:           cwd,
				home:          home,
				force:         force,
				link:          link,
				overwriteHint: "--force",
			}
			if path != "" && !explicitTarget {
				// A bare --path is a verbatim write with no harness
				// semantics: no wrapper, and the JSON target says so.
				spec.target = ""
				spec.reportTarget = "path"
			}

			installed, err := installSoul(cmd, p, client, spec)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(soulInstallResult{Installed: *installed})
			}
			p.Human("Installed %s v%s → %s\n", installed.Name, installed.Version, installed.Path)
			return nil
		},
	}
	cmd.Flags().String("target", "", "install target: hermes, claude, cursor or openclaw (default: detected harness, else hermes)")
	cmd.Flags().String("path", "", "write the SOUL.md to this exact file instead of the target's path")
	cmd.Flags().Bool("force", false, "overwrite an existing file without asking")
	cmd.Flags().Bool("link", false, "claude target only: add an @-import line to ~/.claude/CLAUDE.md")
	return cmd
}

// soulInstallSpec carries one resolved soul-install request into installSoul.
type soulInstallSpec struct {
	slug          string
	target        string // install.Options.Target ("" = bare verbatim write)
	reportTarget  string // the JSON "target" field
	path          string
	cwd, home     string
	force, link   bool
	overwriteHint string
}

// installSoul is the shared machinery behind `soul install` and `init`: the
// JSON detail endpoint validates the slug and supplies name/version (one
// extra GET, deliberately); the counter-bumping .md endpoint is fetched only
// after the overwrite gate passes, inside install.Install.
func installSoul(cmd *cobra.Command, p *output.Printer, client *api.Client, spec soulInstallSpec) (*installedSoul, error) {
	soul, err := client.Soul(cmd.Context(), spec.slug)
	if api.IsNotFound(err) {
		return nil, soulNotFound(cmd.Context(), client, spec.slug)
	}
	if err != nil {
		return nil, err
	}

	res, err := install.Install(install.Options{
		Target:        spec.target,
		Path:          spec.path,
		SoulName:      soul.Name,
		Cwd:           spec.cwd,
		Home:          spec.home,
		Force:         spec.force,
		Link:          spec.link,
		Interactive:   p.Mode.Interactive(),
		Confirm:       confirmFunc(cmd),
		OverwriteHint: spec.overwriteHint,
		Fetch: func() (string, error) {
			return client.SoulMarkdown(cmd.Context(), soul.Slug)
		},
	})
	if err != nil {
		return nil, err
	}
	return &installedSoul{
		Slug:    soul.Slug,
		Name:    soul.Name,
		Version: soul.Version,
		Target:  spec.reportTarget,
		Path:    res.Path,
		Bytes:   res.Bytes,
	}, nil
}

func newLoopInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <slug>",
		Short: "Print a loop's kickoff prompt, ready to paste into your agent",
		Long: "A loop installs as a prompt, not a file: this prints the loop's kickoff prompt " +
			"(or one generated from its goal, check command and exit condition) ready to paste " +
			"into your agent.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			client, err := clientFor(cmd)
			if err != nil {
				return err
			}

			slug := args[0]
			listing, err := fetchTypedListing(cmd.Context(), client, "loop", slug)
			if err != nil {
				return err
			}
			loop, err := listing.LoopData()
			if err != nil {
				return err
			}
			kickoff := loop.Kickoff
			if kickoff == "" {
				kickoff = generatedKickoff(listing, loop)
			}

			if p.Mode.JSON {
				return p.EmitJSON(loopInstallResult{
					Listing: listingRef{Slug: listing.Slug, Name: listing.Name, Type: listing.Type},
					Kickoff: kickoff,
				})
			}
			p.Human("KICKOFF\n%s\n", kickoff)
			return nil
		},
	}
}

// generatedKickoff builds a paste-ready kickoff prompt for a loop that ships
// without one, from whatever recipe fields it has.
func generatedKickoff(l *api.Listing, loop api.LoopData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run the %s loop until its goal is reached.", l.Name)
	if loop.Goal != "" {
		fmt.Fprintf(&b, "\nGoal: %s", loop.Goal)
	}
	if loop.CheckCommand != "" {
		fmt.Fprintf(&b, "\nAfter each iteration, check progress with: %s", loop.CheckCommand)
	}
	if loop.ExitCondition != "" {
		fmt.Fprintf(&b, "\nStop when: %s", loop.ExitCondition)
	}
	if loop.MaxIterations > 0 {
		fmt.Fprintf(&b, "\nGive up after %d iterations and report what is blocking.", loop.MaxIterations)
	}
	return b.String()
}

func newListingInstallCmd(listingType string) *cobra.Command {
	long := fmt.Sprintf("Print the official install command for one %s listing (or its source URL "+
		"when no command is registered). With --run the command is executed via `sh -c` after "+
		"confirmation: interactively you confirm the exact command (skip with --yes); "+
		"non-interactive runs require --yes as the explicit opt-in.", listingType)
	if listingType == "mcp" {
		long += "\n\nNo automatic harness configuration happens in v1 — when Claude Code is " +
			"detected, a ready `claude mcp add` command is suggested instead."
	}
	cmd := &cobra.Command{
		Use:   "install <slug>",
		Short: fmt.Sprintf("Print (or run with --run) the official install command for one %s", listingType),
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := cmd.Flags().GetBool("run")
			if err != nil {
				return err
			}
			yes, err := cmd.Flags().GetBool("yes")
			if err != nil {
				return err
			}
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			client, err := clientFor(cmd)
			if err != nil {
				return err
			}

			slug := args[0]
			listing, err := fetchTypedListing(cmd.Context(), client, listingType, slug)
			if err != nil {
				return err
			}
			installCmd := listingInstallCommand(listing)
			ref := listingRef{Slug: listing.Slug, Name: listing.Name, Type: listing.Type}

			if !run {
				if p.Mode.JSON {
					return p.EmitJSON(listingInstallResult{
						Listing: ref, InstallCmd: installCmd, Executed: false,
					})
				}
				p.Human("%s\n", installCmd)
				if deref(listing.InstallCmd) == "" {
					p.Status("hint: %s has no official install command — its source URL is shown instead\n",
						listing.Slug)
				} else {
					p.Status("hint: re-run with --run to execute\n")
				}
				suggestClaudeMCPAdd(p, listing)
				return nil
			}

			if deref(listing.InstallCmd) == "" {
				return output.ErrorWithHint(
					fmt.Sprintf("%s %q has no official install command to run", listingType, slug),
					"see "+listing.SourceURL)
			}
			if !yes {
				if !p.Mode.Interactive() {
					return output.ErrorWithHint(
						"refusing to run the install command without confirmation",
						fmt.Sprintf("re-run with --yes to execute %q", installCmd))
				}
				ok, err := confirmFunc(cmd)(fmt.Sprintf("Run %q?", installCmd))
				if err != nil {
					return err
				}
				if !ok {
					return output.CancelledError("install cancelled")
				}
			}

			p.Status("Running: %s\n", installCmd)
			// In JSON mode stdout must stay pure JSON, so the command's
			// stdout is streamed to stderr alongside its own stderr.
			cmdStdout := p.Out
			if p.Mode.JSON {
				cmdStdout = p.Err
			}
			exitCode, err := runShell(cmd.Context(), installCmd, cmdStdout, p.Err)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				if err := p.EmitJSON(listingInstallResult{
					Listing: ref, InstallCmd: installCmd, Executed: true, ExitCode: &exitCode,
				}); err != nil {
					return err
				}
			}
			if exitCode != 0 {
				return &output.CodedError{
					Code:    exitCode,
					ErrCode: "error",
					Message: fmt.Sprintf("install command exited with code %d", exitCode),
				}
			}
			if !p.Mode.JSON {
				p.Status("Done.\n")
			}
			return nil
		},
	}
	cmd.Flags().Bool("run", false, "execute the install command via `sh -c` instead of printing it")
	return cmd
}

// fetchTypedListing fetches one listing and enforces the noun's type: a
// missing slug gets the did-you-mean hint, a slug of another type gets the
// cross-type hint pointing at the right noun's install verb.
func fetchTypedListing(ctx context.Context, client *api.Client, listingType, slug string) (*api.Listing, error) {
	listing, err := client.Listing(ctx, slug)
	if api.IsNotFound(err) {
		return nil, listingNotFound(ctx, client, listingType, slug)
	}
	if err != nil {
		return nil, err
	}
	if listing.Type != listingType {
		return nil, output.NotFoundError(
			fmt.Sprintf("%s %q not found", listingType, slug),
			fmt.Sprintf("%q is a %s — run: positronick %s install %s",
				slug, listing.Type, listing.Type, slug))
	}
	return listing, nil
}

// listingInstallCommand returns the listing's official install command, or
// its verified source URL when no command is registered.
func listingInstallCommand(l *api.Listing) string {
	if cmd := deref(l.InstallCmd); cmd != "" {
		return cmd
	}
	return l.SourceURL
}

// lookPath is a seam over exec.LookPath so tests control PATH probing.
var lookPath = exec.LookPath

// suggestClaudeMCPAdd prints the ready `claude mcp add` line when an MCP
// listing is installed on a machine where Claude Code is the detected
// harness and its binary is on PATH — a suggestion, never an auto-config.
func suggestClaudeMCPAdd(p *output.Printer, l *api.Listing) {
	if l.Type != "mcp" || deref(l.InstallCmd) == "" {
		return
	}
	cwd, home, err := resolveCwdHome()
	if err != nil || install.DetectHarness(cwd, home) != "claude" {
		return
	}
	if _, err := lookPath("claude"); err != nil {
		return
	}
	p.Status("hint: add it to Claude Code: claude mcp add %s -- %s\n", l.Slug, *l.InstallCmd)
}

// runShell executes command via `sh -c`, streaming output to the given
// writers, and returns the command's exit code. A package var so tests stub
// execution instead of running real installers.
var runShell = func(ctx context.Context, command string, stdout, stderr io.Writer) (int, error) {
	c := exec.CommandContext(ctx, "sh", "-c", command)
	c.Stdout, c.Stderr = stdout, stderr
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 0, fmt.Errorf("running %q: %w", command, err)
	}
	return 0, nil
}

// resolveCwdHome returns the working and home directories that anchor
// harness detection and the conventional install paths.
func resolveCwdHome() (cwd, home string, err error) {
	if cwd, err = os.Getwd(); err != nil {
		return "", "", fmt.Errorf("resolving working directory: %w", err)
	}
	if home, err = os.UserHomeDir(); err != nil {
		return "", "", fmt.Errorf("resolving home directory: %w", err)
	}
	return cwd, home, nil
}

// confirmFunc builds the y/N confirmation prompt: the question goes to
// stderr, the answer is read from the command's stdin, and EOF means no.
func confirmFunc(cmd *cobra.Command) func(string) (bool, error) {
	return func(prompt string) (bool, error) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N]: ", prompt)
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return false, nil
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes", nil
	}
}
