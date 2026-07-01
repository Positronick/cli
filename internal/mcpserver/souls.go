package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/install"
	"github.com/positronick/cli/internal/search"
)

// soulSearchIn is the soul_search input contract.
type soulSearchIn struct {
	Query     string `json:"query" jsonschema:"text matched fuzzily against soul names, taglines, tags, authors and frameworks"`
	Category  string `json:"category,omitempty" jsonschema:"only souls in this category (case-insensitive)"`
	Framework string `json:"framework,omitempty" jsonschema:"only souls supporting this framework (case-insensitive)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of results"`
}

// soulSummary is the card subset soul_search returns — deliberately slim to
// keep tool-result tokens low; soul_show has the full record.
type soulSummary struct {
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Tagline       string   `json:"tagline"`
	Category      string   `json:"category"`
	Frameworks    []string `json:"frameworks"`
	Version       string   `json:"version"`
	DownloadCount int      `json:"downloadCount"`
}

// soulSearchOut is the soul_search structured output: count is the number of
// results returned, after the limit.
type soulSearchOut struct {
	Count int           `json:"count"`
	Souls []soulSummary `json:"souls"`
}

// soulShowIn is the soul_show input contract.
type soulShowIn struct {
	Slug string `json:"slug" jsonschema:"the soul's slug, as returned by soul_search"`
}

// soulShowOut is the soul_show structured output: the full soul, SOUL.md
// body included.
type soulShowOut struct {
	Soul api.Soul `json:"soul"`
}

// soulInstallIn is the soul_install input contract.
type soulInstallIn struct {
	Slug   string `json:"slug" jsonschema:"the soul's slug, as returned by soul_search"`
	Target string `json:"target,omitempty" jsonschema:"install target (default: the harness detected from marker directories, else hermes)"`
	Path   string `json:"path,omitempty" jsonschema:"write to this exact file instead of the target's conventional path"`
}

// installedSoul mirrors the CLI's `soul install --json` payload subset.
type installedSoul struct {
	Slug   string `json:"slug"`
	Target string `json:"target"`
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
}

// soulInstallOut is the soul_install structured output.
type soulInstallOut struct {
	Installed installedSoul `json:"installed"`
}

// addSoulTools registers soul_search, soul_show and soul_install.
func addSoulTools(srv *mcp.Server, opts Options) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "soul_search",
		Description: "Search positronick.com souls — installable SOUL.md personality files for " +
			"coding agents — ranked by fuzzy relevance. Start here; then call soul_show to " +
			"inspect a result and soul_install to install it.",
		InputSchema: inputSchema[soulSearchIn](func(s *jsonschema.Schema) {
			limitSchema(s.Properties["limit"])
		}),
	}, soulSearchHandler(opts))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "soul_show",
		Description: "Fetch one soul in full, including its verbatim SOUL.md body. Use it to " +
			"inspect a soul_search result before installing.",
		InputSchema: inputSchema[soulShowIn](nil),
	}, soulShowHandler(opts))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "soul_install",
		Description: "Install a soul's SOUL.md where the target harness reads it (counts as a " +
			"download on positronick.com). It never overwrites: an existing file is an error — " +
			"pass a different path or remove the file first. A relative path must resolve " +
			"inside the user's home directory; pass an absolute path to install elsewhere.",
		InputSchema: inputSchema[soulInstallIn](func(s *jsonschema.Schema) {
			s.Properties["target"].Enum = enumOf(install.Targets)
		}),
	}, soulInstallHandler(opts))
}

func soulSearchHandler(opts Options) mcp.ToolHandlerFor[soulSearchIn, soulSearchOut] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in soulSearchIn) (*mcp.CallToolResult, soulSearchOut, error) {
		var out soulSearchOut
		souls, err := opts.Client.Souls(ctx)
		if err != nil {
			return nil, out, err
		}

		kept := make([]api.SoulCard, 0, len(souls))
		for _, s := range souls {
			if in.Category != "" && !strings.EqualFold(s.Category, in.Category) {
				continue
			}
			if in.Framework != "" && !containsFold(s.Frameworks, in.Framework) {
				continue
			}
			kept = append(kept, s)
		}

		matched := capLimit(search.RankSouls(in.Query, kept), in.Limit)
		out = soulSearchOut{Count: len(matched), Souls: make([]soulSummary, len(matched))}
		for i, s := range matched {
			out.Souls[i] = soulSummary{
				Slug:          s.Slug,
				Name:          s.Name,
				Tagline:       s.Tagline,
				Category:      s.Category,
				Frameworks:    s.Frameworks,
				Version:       s.Version,
				DownloadCount: s.DownloadCount,
			}
		}
		return textResult(renderSoulSearch(in.Query, out)), out, nil
	}
}

func soulShowHandler(opts Options) mcp.ToolHandlerFor[soulShowIn, soulShowOut] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in soulShowIn) (*mcp.CallToolResult, soulShowOut, error) {
		var out soulShowOut
		soul, err := opts.Client.Soul(ctx, in.Slug)
		if api.IsNotFound(err) {
			return nil, out, soulNotFoundErr(ctx, opts, in.Slug)
		}
		if err != nil {
			return nil, out, err
		}
		out = soulShowOut{Soul: *soul}
		return textResult(renderSoulDetail(soul)), out, nil
	}
}

func soulInstallHandler(opts Options) mcp.ToolHandlerFor[soulInstallIn, soulInstallOut] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in soulInstallIn) (*mcp.CallToolResult, soulInstallOut, error) {
		var out soulInstallOut

		// Resolve the destination exactly like `soul install`: an explicit
		// path wins; otherwise the target's conventional path, defaulting
		// the target to the detected harness (hermes as fallback). A bare
		// path without a target is a verbatim write reported as "path".
		target, reportTarget := in.Target, in.Target
		if in.Target == "" && in.Path == "" {
			if target = install.DetectHarness(opts.Cwd, opts.Home); target == "" {
				target = "hermes"
			}
			reportTarget = target
		}
		var dest string
		if in.Path == "" {
			var err error
			if dest, err = install.TargetPath(target, opts.Cwd, opts.Home); err != nil {
				return nil, out, err
			}
		} else {
			if in.Target == "" {
				reportTarget = "path"
			}
			var err error
			if dest, err = safeInstallPath(in.Path, opts.Cwd, opts.Home); err != nil {
				return nil, out, err
			}
		}
		// No TTY means no overwrite prompt: an existing file is always an
		// error, decided before the counter-bumping .md fetch.
		if _, err := os.Stat(dest); err == nil {
			return nil, out, fmt.Errorf(
				"%s already exists — pass a different path or remove the file first", dest)
		}

		soul, err := opts.Client.Soul(ctx, in.Slug)
		if api.IsNotFound(err) {
			return nil, out, soulNotFoundErr(ctx, opts, in.Slug)
		}
		if err != nil {
			return nil, out, err
		}

		res, err := install.Install(install.Options{
			Target:   target,
			Path:     dest,
			SoulName: soul.Name,
			Cwd:      opts.Cwd,
			Home:     opts.Home,
			// Fetch runs only after the overwrite gate passes: the .md
			// endpoint bumps the public download counter, and a refused
			// install is not a download.
			Fetch: func() (string, error) {
				return opts.Client.SoulMarkdown(ctx, soul.Slug)
			},
		})
		if err != nil {
			return nil, out, err
		}

		out = soulInstallOut{Installed: installedSoul{
			Slug:   soul.Slug,
			Target: reportTarget,
			Path:   res.Path,
			Bytes:  res.Bytes,
		}}
		return textResult(fmt.Sprintf("Installed %s v%s → %s (%d bytes)",
			soul.Name, soul.Version, res.Path, res.Bytes)), out, nil
	}
}

// soulNotFoundErr builds the tool-error text for a missing soul slug, with
// the same did-you-mean machinery the CLI uses.
func soulNotFoundErr(ctx context.Context, opts Options, slug string) error {
	return notFoundErr(ctx, opts, slug, "soul", func(ctx context.Context) ([]string, error) {
		souls, err := opts.Client.Souls(ctx)
		if err != nil {
			return nil, err
		}
		slugs := make([]string, len(souls))
		for i, s := range souls {
			slugs[i] = s.Slug
		}
		return slugs, nil
	})
}

// safeInstallPath resolves a caller-supplied install path. An absolute path
// is honored as given — explicit intent. A relative path is resolved against
// cwd and must land inside the user's home directory: an MCP server has no
// TTY to confirm a surprising write, so relative escapes are refused.
func safeInstallPath(path, cwd, home string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	resolved := filepath.Join(cwd, path)
	if home == "" || !strings.HasPrefix(resolved, home+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"relative path %q resolves to %s, outside your home directory — pass an absolute path to install there",
			path, resolved)
	}
	return resolved, nil
}

// renderSoulSearch is the compact markdown half of a soul_search result.
func renderSoulSearch(query string, out soulSearchOut) string {
	if out.Count == 0 {
		return fmt.Sprintf("No souls match %q.\n", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s for %q:\n\n", countNoun(out.Count, "soul"), query)
	for _, s := range out.Souls {
		fmt.Fprintf(&b, "- %s — %s v%s (%s, %d downloads): %s\n",
			s.Slug, s.Name, s.Version, s.Category, s.DownloadCount, s.Tagline)
	}
	return b.String()
}

// renderSoulDetail is the markdown half of a soul_show result: a metadata
// header, then the verbatim SOUL.md body.
func renderSoulDetail(s *api.Soul) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (%s)\n\n", s.Name, s.Slug)
	writeField(&b, "author", handleLabel(s.AuthorHandle, deref(s.AuthorName)))
	writeField(&b, "tagline", s.Tagline)
	writeField(&b, "category", s.Category)
	writeField(&b, "frameworks", strings.Join(s.Frameworks, ", "))
	writeField(&b, "version", s.Version)
	writeField(&b, "license", s.License)
	writeField(&b, "downloads", strconv.Itoa(s.DownloadCount))
	fmt.Fprintf(&b, "\n---\n\n%s", s.Content)
	return b.String()
}

// containsFold reports whether values contains want, case-insensitively.
func containsFold(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
