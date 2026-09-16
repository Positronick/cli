package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/install"
)

// skillInstallIn is the skill_install input contract.
type skillInstallIn struct {
	Slug    string `json:"slug" jsonschema:"the skill's slug, as returned by listing_search"`
	Target  string `json:"target,omitempty" jsonschema:"install target (default: agents, the shared standard read by Codex, Cursor, Grok Build and OpenClaw)"`
	Project bool   `json:"project,omitempty" jsonschema:"write the project-local variant under the working directory instead of home"`
	Path    string `json:"path,omitempty" jsonschema:"write to this exact directory instead of the target's conventional path"`
}

// installedSkill mirrors the CLI's `skill install --json` payload subset.
type installedSkill struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Target string `json:"target"`
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
}

// skillInstallOut is the skill_install structured output.
type skillInstallOut struct {
	Installed installedSkill `json:"installed"`
}

// addSkillTools registers skill_install.
func addSkillTools(srv *mcp.Server, opts Options) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "skill_install",
		Description: "Install a skill's hosted SKILL.md where the target agent reads it (counts " +
			"as a download on positronick.com). It never overwrites: an existing file is an " +
			"error — pass a different path or remove the file first. A relative path must " +
			"resolve inside the user's home directory; pass an absolute path to install " +
			"elsewhere. A skill without a hosted asset returns an error; use listing_show to " +
			"see its official install command instead.",
		InputSchema: inputSchema[skillInstallIn](func(s *jsonschema.Schema) {
			s.Properties["target"].Enum = enumOf(install.SkillTargets)
		}),
	}, skillInstallHandler(opts))
}

func skillInstallHandler(opts Options) mcp.ToolHandlerFor[skillInstallIn, skillInstallOut] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in skillInstallIn) (*mcp.CallToolResult, skillInstallOut, error) {
		var out skillInstallOut

		listing, err := opts.Client.Listing(ctx, in.Slug)
		if api.IsNotFound(err) {
			return nil, out, listingNotFoundErr(ctx, opts, in.Slug)
		}
		if err != nil {
			return nil, out, err
		}
		if !listing.HasAsset {
			return nil, out, fmt.Errorf(
				"skill %q has no hosted SKILL.md asset — see its official install command via listing_show",
				in.Slug)
		}

		target, reportTarget := in.Target, in.Target
		if target == "" {
			target = "agents"
			reportTarget = "agents"
		}
		var dir string
		if in.Path == "" {
			if dir, err = install.SkillDir(target, in.Project, opts.Cwd, opts.Home); err != nil {
				return nil, out, err
			}
		} else {
			// safeInstallPath is written for a file path, but it only
			// resolves and bounds-checks — applying it to a directory is
			// the same safety rule: an absolute path is honored as given,
			// a relative one must resolve inside the user's home.
			if dir, err = safeInstallPath(in.Path, opts.Cwd, opts.Home); err != nil {
				return nil, out, err
			}
			reportTarget = "path"
		}

		// No TTY means no overwrite prompt: an existing file is always an
		// error, decided before the counter-bumping .md fetch.
		dest := filepath.Join(dir, listing.Slug, "SKILL.md")
		if _, err := os.Stat(dest); err == nil {
			return nil, out, fmt.Errorf(
				"%s already exists — pass a different path or remove the file first", dest)
		}

		res, err := install.Skill(install.SkillOptions{
			Dir:  dir,
			Slug: listing.Slug,
			Fetch: func() (string, error) {
				return opts.Client.SkillMarkdown(ctx, listing.Slug)
			},
		})
		if err != nil {
			return nil, out, err
		}

		out = skillInstallOut{Installed: installedSkill{
			Slug:   listing.Slug,
			Name:   res.Name,
			Target: reportTarget,
			Path:   res.Path,
			Bytes:  res.Bytes,
		}}
		return textResult(fmt.Sprintf("Installed skill %s → %s (%d bytes)",
			res.Name, res.Path, res.Bytes)), out, nil
	}
}
