package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/install"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// initTopSouls is how many souls the interactive chooser offers.
const initTopSouls = 10

// initSuggestions is how many listings of each suggested type init proposes.
const initSuggestions = 3

// initAction is one entry of the `init --json` actions array: what init did
// ("soul_install") or what it proposes next ("suggestion").
type initAction struct {
	Kind       string `json:"kind"`
	Type       string `json:"type,omitempty"`
	Slug       string `json:"slug"`
	Path       string `json:"path,omitempty"`
	InstallCmd string `json:"installCmd,omitempty"`
}

// initResult is the `init --json` contract:
// {"harness":"...","actions":[...]}. Harness is "" when none was detected.
type initResult struct {
	Harness string       `json:"harness"`
	Actions []initAction `json:"actions"`
}

const initLong = `Set up this machine for agent capabilities, in one command:

1. detect your harness from marker directories (.hermes, .claude, .cursor,
   .openclaw) in the working directory, then your home directory;
2. install a soul there — --soul picks it, otherwise an interactive run
   offers the top downloads and a non-interactive run requires --soul;
3. suggest the top MCP servers and skills with their install commands as
   next steps (nothing is executed).

--yes answers the overwrite confirmation when a SOUL.md already exists.`

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up this machine: detect the harness, install a soul, suggest tooling",
		Long:  initLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			soulFlag, err := cmd.Flags().GetString("soul")
			if err != nil {
				return err
			}
			target, err := cmd.Flags().GetString("target")
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

			cwd, home, err := resolveCwdHome()
			if err != nil {
				return err
			}
			harness := install.DetectHarness(cwd, home)
			if !cmd.Flags().Changed("target") {
				if target = harness; target == "" {
					target = "hermes"
				}
			} else if _, err := install.TargetPath(target, cwd, home); err != nil {
				return output.Errorf("%s", err)
			}

			slug := soulFlag
			if slug == "" {
				if !p.Mode.Interactive() {
					return output.ErrorWithHint("init needs a soul to install",
						"re-run with --soul <slug> (browse with: positronick soul list --sort downloads)")
				}
				souls, err := client.Souls(cmd.Context())
				if err != nil {
					return err
				}
				reorder(souls, "downloads",
					func(s api.SoulCard) string { return s.Name },
					func(s api.SoulCard) int { return s.DownloadCount },
					func(s api.SoulCard) string { return s.CreatedAt })
				if slug, err = chooseSoul(cmd.InOrStdin(), cmd.ErrOrStderr(),
					applyLimit(souls, initTopSouls)); err != nil {
					return err
				}
			}

			installed, err := installSoul(cmd, p, client, soulInstallSpec{
				slug:          slug,
				target:        target,
				reportTarget:  target,
				cwd:           cwd,
				home:          home,
				force:         yes,
				overwriteHint: "--yes",
			})
			if err != nil {
				return err
			}

			suggestions, err := initSuggestionActions(cmd.Context(), client)
			if err != nil {
				return err
			}
			actions := append([]initAction{{
				Kind: "soul_install", Slug: installed.Slug, Path: installed.Path,
			}}, suggestions...)

			if p.Mode.JSON {
				return p.EmitJSON(initResult{Harness: harness, Actions: actions})
			}
			if harness == "" {
				p.Human("No harness detected — using %s.\n", target)
			} else {
				p.Human("Detected harness: %s\n", harness)
			}
			p.Human("Installed %s v%s → %s\n", installed.Name, installed.Version, installed.Path)
			if len(suggestions) > 0 {
				p.Human("\nNEXT STEPS\n")
				rows := make([][]string, len(suggestions))
				for i, s := range suggestions {
					rows[i] = []string{s.Type, s.Slug, s.InstallCmd}
				}
				output.RenderTable(p.Out, []string{"TYPE", "SLUG", "INSTALL"}, rows)
			}
			return nil
		},
	}
	cmd.Flags().String("soul", "", "slug of the soul to install (required when non-interactive)")
	cmd.Flags().String("target", "", "install target: hermes, claude, cursor or openclaw (default: detected harness, else hermes)")
	return cmd
}

// initSuggestionActions proposes the top-downloaded MCP servers and skills,
// each with its official install command (or source URL) — printed, never
// executed.
func initSuggestionActions(ctx context.Context, client *api.Client) ([]initAction, error) {
	var actions []initAction
	for _, listingType := range []string{"mcp", "skill"} {
		listings, err := client.Listings(ctx, listingType)
		if err != nil {
			return nil, err
		}
		reorder(listings, "downloads",
			func(l api.Listing) string { return l.Name },
			func(l api.Listing) int { return l.DownloadCount },
			func(l api.Listing) string { return l.CreatedAt })
		for _, l := range applyLimit(listings, initSuggestions) {
			actions = append(actions, initAction{
				Kind:       "suggestion",
				Type:       l.Type,
				Slug:       l.Slug,
				InstallCmd: listingInstallCommand(&l),
			})
		}
	}
	return actions, nil
}

// chooseSoul renders a numbered top-souls list on errW and reads a 1-based
// selection from in. EOF cancels; anything unparsable fails loud.
func chooseSoul(in io.Reader, errW io.Writer, souls []api.SoulCard) (string, error) {
	if len(souls) == 0 {
		return "", output.Errorf("no souls available to choose from")
	}
	fmt.Fprintln(errW, "Choose a soul to install:")
	for i, s := range souls {
		fmt.Fprintf(errW, "%3d. %s — %s\n", i+1, s.Name, s.Tagline)
	}
	fmt.Fprintf(errW, "Soul number [1-%d]: ", len(souls))

	line, err := bufio.NewReader(in).ReadString('\n')
	answer := strings.TrimSpace(line)
	if err != nil && answer == "" {
		return "", output.CancelledError("init cancelled")
	}
	n, convErr := strconv.Atoi(answer)
	if convErr != nil || n < 1 || n > len(souls) {
		return "", output.Errorf("invalid selection %q (expected 1-%d)", answer, len(souls))
	}
	return souls[n-1].Slug, nil
}
