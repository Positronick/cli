package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/positronick/cli/internal/search"
	"github.com/spf13/cobra"
)

// listingSearchResult is the `<noun> search --json` contract:
// {"query":"...","count":N,"listings":[Listing...]}.
type listingSearchResult struct {
	Query    string        `json:"query"`
	Count    int           `json:"count"`
	Listings []api.Listing `json:"listings"`
}

// listingListResult is the `<noun> list --json` contract — search minus
// "query".
type listingListResult struct {
	Count    int           `json:"count"`
	Listings []api.Listing `json:"listings"`
}

// listingDetail is the `<noun> show --json` contract: {"listing":{...}}.
type listingDetail struct {
	Listing api.Listing `json:"listing"`
}

// listingNounShorts gives each registry noun its own help line.
var listingNounShorts = map[string]string{
	"harness": "Discover agent harnesses in the registry",
	"cli":     "Discover official CLI tools in the registry",
	"mcp":     "Discover MCP servers in the registry",
	"agent":   "Discover agent SDKs and frameworks in the registry",
	"skill":   "Discover agent skills in the registry",
	"plugin":  "Discover agent plugins in the registry",
	"loop":    "Discover reusable agent loops in the registry",
}

// newListingNounCmd builds one registry noun (harness, cli, mcp, agent,
// skill, plugin, loop) with the shared search/list/show verbs, each scoped to
// its listing type via ?type= on the API.
func newListingNounCmd(listingType string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   listingType,
		Short: listingNounShorts[listingType],
	}
	cmd.AddCommand(
		newListingSearchCmd(listingType),
		newListingListCmd(listingType),
		newListingShowCmd(listingType),
	)
	return cmd
}

func newListingSearchCmd(listingType string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: fmt.Sprintf("Search %s listings by fuzzy relevance", listingType),
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags, err := parseReadFlags(cmd, false)
			if err != nil {
				return err
			}
			query, err := searchQuery(listingType, args)
			if err != nil {
				return err
			}
			matched, p, err := fetchListings(cmd, listingType, query, flags)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(listingSearchResult{Query: query, Count: len(matched), Listings: matched})
			}
			renderListingTable(p, matched)
			return nil
		},
	}
	addReadFlags(cmd, "relevance", false)
	return cmd
}

func newListingListCmd(listingType string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List all %s listings", listingType),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags, err := parseReadFlags(cmd, false)
			if err != nil {
				return err
			}
			matched, p, err := fetchListings(cmd, listingType, "", flags)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(listingListResult{Count: len(matched), Listings: matched})
			}
			renderListingTable(p, matched)
			return nil
		},
	}
	addReadFlags(cmd, "name", false)
	return cmd
}

// fetchListings runs the shared search/list pipeline for one listing type:
// fetch the type's listings (?type= on the server), apply the category
// filter, rank/sort client-side, and truncate to the limit.
func fetchListings(cmd *cobra.Command, listingType, query string, flags readFlags) ([]api.Listing, *output.Printer, error) {
	p, err := printerFor(cmd)
	if err != nil {
		return nil, nil, err
	}
	client, err := clientFor(cmd)
	if err != nil {
		return nil, nil, err
	}
	listings, err := client.Listings(cmd.Context(), listingType)
	if err != nil {
		return nil, nil, err
	}

	kept := make([]api.Listing, 0, len(listings))
	for _, l := range listings {
		if flags.category != "" && !strings.EqualFold(l.Category, flags.category) {
			continue
		}
		kept = append(kept, l)
	}

	matched := search.RankListings(query, kept)
	reorder(matched, flags.sort,
		func(l api.Listing) string { return l.Name },
		func(l api.Listing) int { return l.DownloadCount },
		func(l api.Listing) string { return l.CreatedAt })
	return applyLimit(matched, flags.limit), p, nil
}

func renderListingTable(p *output.Printer, listings []api.Listing) {
	rows := make([][]string, len(listings))
	for i, l := range listings {
		rows[i] = []string{l.Name, l.Slug, truncateCell(l.Tagline, taglineWidth)}
	}
	output.RenderTable(p.Out, []string{"NAME", "SLUG", "TAGLINE"}, rows)
}

func newListingShowCmd(listingType string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: fmt.Sprintf("Show one %s listing in full", listingType),
		Args:  cobra.ExactArgs(1),
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
			listing, err := client.Listing(cmd.Context(), slug)
			if api.IsNotFound(err) {
				return listingNotFound(cmd.Context(), client, listingType, slug)
			}
			if err != nil {
				return err
			}
			// The slug namespace is shared across types; a slug from another
			// noun is "not found" here, with a hint to the right noun.
			if listing.Type != listingType {
				return output.NotFoundError(
					fmt.Sprintf("%s %q not found", listingType, slug),
					fmt.Sprintf("%q is a %s — run: positronick %s show %s",
						slug, listing.Type, listing.Type, slug))
			}

			if p.Mode.JSON {
				return p.EmitJSON(listingDetail{Listing: *listing})
			}
			return renderListingDetail(p, listing)
		},
	}
}

// listingNotFound builds the exit-3 error for a missing slug, suggesting the
// closest slug of the same type. A failing suggestion fetch never masks the
// original not-found.
func listingNotFound(ctx context.Context, client *api.Client, listingType, slug string) error {
	hint := fmt.Sprintf("Run: positronick %s list", listingType)
	if listings, err := client.Listings(ctx, listingType); err == nil {
		slugs := make([]string, len(listings))
		for i, l := range listings {
			slugs[i] = l.Slug
		}
		if match := closestSlug(slug, slugs); match != "" {
			hint = fmt.Sprintf("did you mean %q? %s", match, hint)
		}
	}
	return output.NotFoundError(fmt.Sprintf("%s %q not found", listingType, slug), hint)
}

// renderListingDetail prints the human field view. For loops it also renders
// the LoopData recipe — goal, check command, exit condition, iteration cap,
// compatible tools, and the kickoff prompt — which is the entire point of
// `positronick loop show`.
func renderListingDetail(p *output.Printer, l *api.Listing) error {
	official := "no"
	if l.Official {
		official = "yes"
	}
	rows := fieldRows(
		"NAME", l.Name,
		"SLUG", l.Slug,
		"TYPE", l.Type,
		"PROFILE", handleLabel(l.ProfileHandle, l.ProfileName),
		"TAGLINE", l.Tagline,
		"DESCRIPTION", deref(l.Description),
		"CATEGORY", l.Category,
		"TAGS", strings.Join(l.Tags, ", "),
		"OFFICIAL", official,
		"SOURCE", l.SourceURL,
		"REPO", deref(l.RepoURL),
		"INSTALL", deref(l.InstallCmd),
		"CONFIDENCE", l.Confidence,
		"STATUS", l.Status,
		"DOWNLOADS", strconv.Itoa(l.DownloadCount),
		"CREATED", l.CreatedAt,
		"UPDATED", l.UpdatedAt,
	)

	var loop api.LoopData
	if l.Type == "loop" {
		var err error
		if loop, err = l.LoopData(); err != nil {
			return err
		}
		maxIterations := ""
		if loop.MaxIterations > 0 {
			maxIterations = strconv.Itoa(loop.MaxIterations)
		}
		rows = append(rows, fieldRows(
			"GOAL", loop.Goal,
			"CHECK COMMAND", loop.CheckCommand,
			"EXIT CONDITION", loop.ExitCondition,
			"MAX ITERATIONS", maxIterations,
			"COMPATIBLE TOOLS", strings.Join(loop.CompatibleTools, ", "),
		)...)
	}

	output.RenderFields(p.Out, rows)
	if loop.Kickoff != "" {
		p.Human("\nKICKOFF\n%s\n", loop.Kickoff)
	}
	return nil
}
