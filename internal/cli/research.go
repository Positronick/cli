package cli

import (
	"slices"
	"strings"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// This file owns the public `research` command — the agent-facing "what's new"
// feed over positronick.com's published blog posts (articles, mirrored
// releases, mirrored links). It is read-only and unauthenticated, mirroring the
// soul/listing read commands. Unlike those, the server does the filtering:
// --since/--kind/--category/--tag/--limit/query all travel on the wire, and the
// response carries a `latest` high-water mark to poll only the delta next time.

const (
	// researchTitleWidth is the rune budget for the TITLE column, keeping
	// PUBLISHED + KIND + TITLE + SLUG inside a 100-column terminal.
	researchTitleWidth = 50
	// maxResearchLimit is the server's documented upper bound on --limit.
	maxResearchLimit = 100
)

// researchResult is the `research --json` contract:
// {"count":N,"latest":"<iso>"|null,"results":[ResearchItem...]}.
type researchResult struct {
	Count   int                `json:"count"`
	Latest  *string            `json:"latest"`
	Results []api.ResearchItem `json:"results"`
}

func newResearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "research [query]",
		Short: "List what's new on positronick.com (articles, releases, links)",
		Long: "Fetch the agent-facing \"what's new\" feed: published blog posts, mirrored GitHub " +
			"releases, and mirrored news links, newest first. Filter with --kind/--category/--tag " +
			"or a free-text query, and poll just the delta with --since <iso> — the printed " +
			"`latest` timestamp is the value to pass next time. Read-only; never prompts.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := parseResearchQuery(cmd, args)
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

			res, err := client.Research(cmd.Context(), q)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(researchResult{Count: len(res.Results), Latest: res.Latest, Results: res.Results})
			}
			renderResearchTable(p, res)
			return nil
		},
	}
	f := cmd.Flags()
	f.String("kind", "", "only this post kind: article, release or link")
	f.String("category", "", "only posts in this category")
	f.String("tag", "", "only posts carrying this tag")
	f.String("since", "", "only posts published after this ISO-8601 instant (poll the delta)")
	f.Int("limit", defaultLimit, "maximum number of results (1–100)")
	return cmd
}

// parseResearchQuery reads and validates the research flags + positional query,
// failing loud — before any network call — on an out-of-range limit or an
// unknown kind, with the valid set named.
func parseResearchQuery(cmd *cobra.Command, args []string) (api.ResearchQuery, error) {
	var q api.ResearchQuery
	var err error
	q.Q = strings.TrimSpace(strings.Join(args, " "))
	if q.Kind, err = cmd.Flags().GetString("kind"); err != nil {
		return q, err
	}
	if q.Category, err = cmd.Flags().GetString("category"); err != nil {
		return q, err
	}
	if q.Tag, err = cmd.Flags().GetString("tag"); err != nil {
		return q, err
	}
	if q.Since, err = cmd.Flags().GetString("since"); err != nil {
		return q, err
	}
	if q.Limit, err = cmd.Flags().GetInt("limit"); err != nil {
		return q, err
	}
	if q.Limit < 1 || q.Limit > maxResearchLimit {
		return q, output.Errorf("--limit must be between 1 and %d, got %d", maxResearchLimit, q.Limit)
	}
	if q.Kind != "" && !slices.Contains(api.PostKinds, q.Kind) {
		return q, output.Errorf("invalid --kind %q (valid: %s)", q.Kind, strings.Join(api.PostKinds, ", "))
	}
	return q, nil
}

func renderResearchTable(p *output.Printer, res *api.ResearchResult) {
	rows := make([][]string, len(res.Results))
	for i, it := range res.Results {
		rows[i] = []string{researchDate(it.PublishedAt), it.Kind, truncateCell(it.Title, researchTitleWidth), it.Slug}
	}
	output.RenderTable(p.Out, []string{"PUBLISHED", "KIND", "TITLE", "SLUG"}, rows)
	if res.Latest != nil {
		p.Status("latest: %s — poll the delta next time with --since %s\n", *res.Latest, *res.Latest)
	}
}

// researchDate renders a post's publish instant as its calendar date (the
// leading YYYY-MM-DD of the ISO-8601 string), or "" when unset.
func researchDate(publishedAt *string) string {
	if publishedAt == nil || len(*publishedAt) < 10 {
		return deref(publishedAt)
	}
	return (*publishedAt)[:10]
}
