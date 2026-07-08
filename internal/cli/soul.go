package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/positronick/cli/internal/search"
	"github.com/spf13/cobra"
)

// soulSearchResult is the `soul search --json` contract:
// {"query":"...","count":N,"souls":[SoulCard...]}.
type soulSearchResult struct {
	Query string         `json:"query"`
	Count int            `json:"count"`
	Souls []api.SoulCard `json:"souls"`
}

// soulListResult is the `soul list --json` contract — search minus "query".
type soulListResult struct {
	Count int            `json:"count"`
	Souls []api.SoulCard `json:"souls"`
}

// soulDetail is the `soul show --json` contract: {"soul":{...}}.
type soulDetail struct {
	Soul api.Soul `json:"soul"`
}

func newSoulCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soul",
		Short: "Discover SOUL.md personality files",
	}
	cmd.AddCommand(newSoulSearchCmd(), newSoulListCmd(), newSoulShowCmd())
	return cmd
}

func newSoulSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search souls by fuzzy relevance",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags, err := parseReadFlags(cmd, true)
			if err != nil {
				return err
			}
			query, err := searchQuery("soul", args)
			if err != nil {
				return err
			}
			matched, p, err := fetchSouls(cmd, query, flags)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(soulSearchResult{Query: query, Count: len(matched), Souls: matched})
			}
			renderSoulTable(p, matched)
			return nil
		},
	}
	addReadFlags(cmd, "relevance", true)
	return cmd
}

func newSoulListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all souls",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags, err := parseReadFlags(cmd, true)
			if err != nil {
				return err
			}
			matched, p, err := fetchSouls(cmd, "", flags)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(soulListResult{Count: len(matched), Souls: matched})
			}
			renderSoulTable(p, matched)
			return nil
		},
	}
	addReadFlags(cmd, "name", true)
	return cmd
}

// fetchSouls runs the shared search/list pipeline: fetch the full gallery,
// apply facet filters, rank/sort (client-side by design at this catalog
// size — the server reserves ?q= for later), and truncate to the limit.
func fetchSouls(cmd *cobra.Command, query string, flags readFlags) ([]api.SoulCard, *output.Printer, error) {
	p, err := printerFor(cmd)
	if err != nil {
		return nil, nil, err
	}
	client, err := clientFor(cmd)
	if err != nil {
		return nil, nil, err
	}
	souls, err := client.Souls(cmd.Context())
	if err != nil {
		return nil, nil, err
	}

	kept := make([]api.SoulCard, 0, len(souls))
	for _, s := range souls {
		if flags.category != "" && !strings.EqualFold(s.Category, flags.category) {
			continue
		}
		if flags.framework != "" && !containsFold(s.Frameworks, flags.framework) {
			continue
		}
		kept = append(kept, s)
	}

	matched := search.RankSouls(query, kept)
	reorder(matched, flags.sort,
		func(s api.SoulCard) string { return s.Name },
		func(s api.SoulCard) int { return s.DownloadCount },
		func(s api.SoulCard) string { return s.CreatedAt })
	return applyLimit(matched, flags.limit), p, nil
}

func renderSoulTable(p *output.Printer, souls []api.SoulCard) {
	rows := make([][]string, len(souls))
	for i, s := range souls {
		rows[i] = []string{s.Name, s.Slug, truncateCell(s.Tagline, taglineWidth)}
	}
	output.RenderTable(p.Out, []string{"NAME", "SLUG", "TAGLINE"}, rows)
}

func newSoulShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <slug>",
		Short: "Show one soul in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := cmd.Flags().GetBool("raw")
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
			// Always the JSON detail endpoint — never the .md endpoint, which
			// bumps the soul's install counter.
			soul, err := client.Soul(cmd.Context(), slug)
			if api.IsNotFound(err) {
				return soulNotFound(cmd.Context(), client, slug)
			}
			if err != nil {
				return err
			}

			if raw {
				p.Human("%s", soul.Content)
				return nil
			}
			if p.Mode.JSON {
				return p.EmitJSON(soulDetail{Soul: *soul})
			}
			renderSoulDetail(p, soul)
			return nil
		},
	}
	cmd.Flags().Bool("raw", false, "print only the SOUL.md body verbatim (overrides --json)")
	return cmd
}

// soulNotFound builds the exit-3 error for a missing slug, with a did-you-mean
// hint when the gallery has a plausible neighbor. See notFoundWithSuggestion.
func soulNotFound(ctx context.Context, client *api.Client, slug string) error {
	souls, err := client.Souls(ctx)
	slugs := make([]string, len(souls))
	for i, s := range souls {
		slugs[i] = s.Slug
	}
	return notFoundWithSuggestion("soul", slug, slugs, err)
}

func renderSoulDetail(p *output.Printer, s *api.Soul) {
	authorName := deref(s.AuthorName)
	output.RenderFields(p.Out, fieldRows(
		"NAME", s.Name,
		"SLUG", s.Slug,
		"AUTHOR", handleLabel(s.AuthorHandle, authorName),
		"TAGLINE", s.Tagline,
		"DESCRIPTION", deref(s.Description),
		"CATEGORY", s.Category,
		"TAGS", strings.Join(s.Tags, ", "),
		"FRAMEWORKS", strings.Join(s.Frameworks, ", "),
		"MODELS", strings.Join(s.Models, ", "),
		"VERSION", s.Version,
		"LICENSE", s.License,
		"REPO", deref(s.RepoURL),
		"DOWNLOADS", strconv.Itoa(s.DownloadCount),
		"CHARGES", strconv.Itoa(s.ChargeCount),
		"STATUS", s.Status,
		"CREATED", s.CreatedAt,
		"UPDATED", s.UpdatedAt,
	))
	p.Status("hint: run `positronick soul show %s --raw` to print the SOUL.md body\n", s.Slug)
}
