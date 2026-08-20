package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/search"
)

// listingSearchIn is the listing_search input contract.
type listingSearchIn struct {
	Query    string `json:"query" jsonschema:"text matched fuzzily against listing names, taglines, publishers, types and tags"`
	Type     string `json:"type,omitempty" jsonschema:"only listings of this type"`
	Category string `json:"category,omitempty" jsonschema:"only listings in this category (case-insensitive)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum number of results"`
}

// listingSummary is the slim subset listing_search returns; listing_show has
// the full record.
type listingSummary struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Tagline    string `json:"tagline"`
	InstallCmd string `json:"installCmd,omitempty"`
	SourceURL  string `json:"sourceUrl"`
}

// listingSearchOut is the listing_search structured output: count is the
// number of results returned, after the limit.
type listingSearchOut struct {
	Count    int              `json:"count"`
	Listings []listingSummary `json:"listings"`
}

// listingShowIn is the listing_show input contract.
type listingShowIn struct {
	Slug string `json:"slug" jsonschema:"the listing's slug, as returned by listing_search"`
}

// listingShowOut is the listing_show structured output: the full listing,
// type-specific data payload included.
type listingShowOut struct {
	Listing api.Listing `json:"listing"`
}

// addListingTools registers listing_search and listing_show.
func addListingTools(srv *mcp.Server, opts Options) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "listing_search",
		Description: "Search positronick.com's registry of verified agent tooling — harnesses, " +
			"CLIs, MCP servers, memory, agent SDKs, skills, plugins, loops and bots — ranked by fuzzy " +
			"relevance. Follow up with listing_show for the full record.",
		InputSchema: inputSchema[listingSearchIn](func(s *jsonschema.Schema) {
			s.Properties["type"].Enum = enumOf(api.ListingTypes)
			limitSchema(s.Properties["limit"])
		}),
	}, listingSearchHandler(opts))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "listing_show",
		Description: "Fetch one registry listing in full, including its type-specific data — " +
			"for loops that means the recipe and the ready-to-paste kickoff prompt.",
		InputSchema: inputSchema[listingShowIn](nil),
	}, listingShowHandler(opts))
}

func listingSearchHandler(opts Options) mcp.ToolHandlerFor[listingSearchIn, listingSearchOut] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listingSearchIn) (*mcp.CallToolResult, listingSearchOut, error) {
		var out listingSearchOut
		// The type scope is server-side (?type=), exactly like the CLI nouns.
		listings, err := opts.Client.Listings(ctx, in.Type)
		if err != nil {
			return nil, out, err
		}

		kept := make([]api.Listing, 0, len(listings))
		for _, l := range listings {
			if in.Category != "" && !strings.EqualFold(l.Category, in.Category) {
				continue
			}
			kept = append(kept, l)
		}

		matched := capLimit(search.RankListings(in.Query, kept), in.Limit)
		out = listingSearchOut{Count: len(matched), Listings: make([]listingSummary, len(matched))}
		for i, l := range matched {
			out.Listings[i] = listingSummary{
				Slug:       l.Slug,
				Name:       l.Name,
				Type:       l.Type,
				Tagline:    l.Tagline,
				InstallCmd: deref(l.InstallCmd),
				SourceURL:  l.SourceURL,
			}
		}
		return textResult(renderListingSearch(in.Query, out)), out, nil
	}
}

func listingShowHandler(opts Options) mcp.ToolHandlerFor[listingShowIn, listingShowOut] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listingShowIn) (*mcp.CallToolResult, listingShowOut, error) {
		var out listingShowOut
		listing, err := opts.Client.Listing(ctx, in.Slug)
		if api.IsNotFound(err) {
			return nil, out, listingNotFoundErr(ctx, opts, in.Slug)
		}
		if err != nil {
			return nil, out, err
		}
		loop, err := listing.LoopData()
		if err != nil {
			return nil, out, err
		}
		out = listingShowOut{Listing: *listing}
		return textResult(renderListingDetail(listing, loop)), out, nil
	}
}

// listingNotFoundErr builds the tool-error text for a missing listing slug,
// suggesting the closest slug across the whole registry.
func listingNotFoundErr(ctx context.Context, opts Options, slug string) error {
	return notFoundErr(ctx, opts, slug, "listing", func(ctx context.Context) ([]string, error) {
		listings, err := opts.Client.Listings(ctx, "")
		if err != nil {
			return nil, err
		}
		slugs := make([]string, len(listings))
		for i, l := range listings {
			slugs[i] = l.Slug
		}
		return slugs, nil
	})
}

// renderListingSearch is the compact markdown half of a listing_search
// result.
func renderListingSearch(query string, out listingSearchOut) string {
	if out.Count == 0 {
		return fmt.Sprintf("No listings match %q.\n", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s for %q:\n\n", countNoun(out.Count, "listing"), query)
	for _, l := range out.Listings {
		fmt.Fprintf(&b, "- %s (%s) — %s: %s", l.Slug, l.Type, l.Name, l.Tagline)
		if l.InstallCmd != "" {
			fmt.Fprintf(&b, " — install: %s", l.InstallCmd)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderListingDetail is the markdown half of a listing_show result. For
// loops it renders the recipe fields and the kickoff prompt — the entire
// point of a loop listing.
func renderListingDetail(l *api.Listing, loop api.LoopData) string {
	official := ""
	if l.Official {
		official = "yes"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (%s)\n\n", l.Name, l.Slug)
	writeField(&b, "type", l.Type)
	writeField(&b, "publisher", handleLabel(l.ProfileHandle, l.ProfileName))
	writeField(&b, "tagline", l.Tagline)
	writeField(&b, "description", deref(l.Description))
	writeField(&b, "category", l.Category)
	writeField(&b, "official", official)
	writeField(&b, "source", l.SourceURL)
	writeField(&b, "install", deref(l.InstallCmd))
	writeField(&b, "downloads", strconv.Itoa(l.DownloadCount))
	if l.Type == "loop" {
		maxIterations := ""
		if loop.MaxIterations > 0 {
			maxIterations = strconv.Itoa(loop.MaxIterations)
		}
		writeField(&b, "goal", loop.Goal)
		writeField(&b, "check command", loop.CheckCommand)
		writeField(&b, "exit condition", loop.ExitCondition)
		writeField(&b, "max iterations", maxIterations)
		writeField(&b, "compatible tools", strings.Join(loop.CompatibleTools, ", "))
		if loop.Kickoff != "" {
			fmt.Fprintf(&b, "\n## Kickoff\n\n%s\n", loop.Kickoff)
		}
	}
	return b.String()
}
