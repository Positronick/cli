package cli

import (
	"sort"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// This file owns the hidden admin `feed` command group (list/create/update/
// sync) against /api/admin/feeds — the blog feed sources (GitHub release / RSS
// mirroring) the ingestor polls. Like the other admin commands they are
// Hidden + annotated (revealed for cached admins) and map a 401/403 to exit 4
// via adminAPIError. There is no delete verb: --enabled=false pauses a feed.
// There is deliberately no ingest-all command here — ingesting every feed on a
// schedule is the GitHub Actions cron's job (it holds the admin key as a
// secret); a human operator syncs a single feed at a time.

// feedCreateResult is the `feed create --json` contract.
type feedCreateResult struct {
	Feed    api.FeedSource `json:"feed"`
	Created bool           `json:"created"`
}

// feedUpdateResult is the `feed update --json` contract — the server's
// response shape (a feed source has no ownership flip to report).
type feedUpdateResult struct {
	Feed api.FeedSource `json:"feed"`
}

// feedListResult is the `feed list --json` contract.
type feedListResult struct {
	Count int              `json:"count"`
	Feeds []api.FeedSource `json:"feeds"`
}

// feedSyncResult is the `feed sync --json` contract.
type feedSyncResult struct {
	Summary api.FeedSyncSummary `json:"summary"`
}

// feedStringFlagMap maps the feed string-field flags to their API field names
// (the repeatable --tag and the bool flags are handled separately).
var feedStringFlagMap = [][2]string{
	{"label", "label"},
	{"feed-url", "feedUrl"},
	{"kind", "kind"},
	{"category", "defaultCategory"},
	{"author", "authorHandle"},
	{"listing", "listingSlug"},
}

func newFeedCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "feed",
		Short: "Manage blog feed sources (admin)",
		Long: "Manage the release/RSS feed sources the blog ingestor mirrors into posts: list them, " +
			"create one, pause one with --enabled=false, or sync one on demand. Ingesting every feed " +
			"on a schedule is the cron's job, not a CLI command." + adminNote,
	})
	cmd.AddCommand(newFeedListCmd(), newFeedCreateCmd(), newFeedUpdateCmd(), newFeedSyncCmd())
	return cmd
}

// addFeedFieldFlags registers the per-field flags shared by feed create and
// feed update. The bool defaults mirror the server's (autoPublish false,
// enabled true) for create; update sends a bool only when it is set.
func addFeedFieldFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("label", "", "admin display label for the feed")
	f.String("feed-url", "", "github_release repo URL or rss feed URL")
	f.String("kind", "", "feed kind: github_release or rss")
	f.String("category", "", "default blog category for mirrored posts (e.g. Releases)")
	f.String("author", "", "default author profile handle stamped on mirrored posts")
	f.String("listing", "", "related listing slug stamped on mirrored posts")
	f.StringArray("tag", nil, "default tag for mirrored posts, repeatable")
	f.Bool("auto-publish", false, "publish mirrored posts immediately instead of as drafts")
	f.Bool("enabled", true, "whether the feed is active (set --enabled=false to pause)")
}

// applyFeedStringFlags copies the string field flags into fields. On create
// every non-empty value is sent and empty optionals are omitted (the server
// applies its defaults); on update only the flags explicitly set are sent, so
// an explicit empty --author/--listing clears that attribution.
func applyFeedStringFlags(cmd *cobra.Command, fields map[string]any, forCreate bool) error {
	return applyStringFieldFlags(cmd, feedStringFlagMap, fields, forCreate)
}

// applyFeedTagsFlag copies --tag into defaultTags when explicitly set.
func applyFeedTagsFlag(cmd *cobra.Command, fields map[string]any) error {
	if !cmd.Flags().Changed("tag") {
		return nil
	}
	tags, err := cmd.Flags().GetStringArray("tag")
	if err != nil {
		return err
	}
	fields["defaultTags"] = tags
	return nil
}

// applyFeedBoolFlag copies a bool flag into fields under key when explicitly set.
func applyFeedBoolFlag(cmd *cobra.Command, name, key string, fields map[string]any) error {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		return err
	}
	fields[key] = v
	return nil
}

func newFeedCreateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "create --label LABEL --feed-url URL --kind github_release|rss --category CATEGORY",
		Short: "Create a blog feed source (admin)",
		Long: "Subscribe to a release/RSS feed the ingestor mirrors into blog posts. --author and " +
			"--listing set the default byline and related tool stamped on every mirrored post; --tag " +
			"is repeatable. Mirrored posts are drafts unless --auto-publish is set. Validation is the " +
			"server's; a 422 carries its message verbatim." + adminNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			fields := map[string]any{}
			if err := applyFeedStringFlags(cmd, fields, true); err != nil {
				return err
			}
			if err := applyFeedTagsFlag(cmd, fields); err != nil {
				return err
			}
			// On create the bool defaults are meaningful, so both are always sent.
			autoPublish, err := cmd.Flags().GetBool("auto-publish")
			if err != nil {
				return err
			}
			enabled, err := cmd.Flags().GetBool("enabled")
			if err != nil {
				return err
			}
			fields["autoPublish"] = autoPublish
			fields["enabled"] = enabled

			feed, err := client.CreateFeed(cmd.Context(), fields)
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(feedCreateResult{Feed: *feed, Created: true})
			}
			renderFeedSaved(p, "Created", feed)
			return nil
		},
	})
	addFeedFieldFlags(cmd)
	for _, required := range []string{"label", "feed-url", "kind", "category"} {
		_ = cmd.MarkFlagRequired(required)
	}
	return cmd
}

func newFeedUpdateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "update <id>",
		Short: "Update or pause a blog feed source (admin)",
		Long: "Patch a feed source in place: only the fields you provide change. Set --enabled=false " +
			"to pause a feed (there is no delete verb) and --enabled=true to resume it. An empty " +
			"--author or --listing clears that attribution. The argument is the feed id — feeds have " +
			"no public slug." + adminNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			patch := map[string]any{}
			if err := applyFeedStringFlags(cmd, patch, false); err != nil {
				return err
			}
			if err := applyFeedTagsFlag(cmd, patch); err != nil {
				return err
			}
			if err := applyFeedBoolFlag(cmd, "auto-publish", "autoPublish", patch); err != nil {
				return err
			}
			if err := applyFeedBoolFlag(cmd, "enabled", "enabled", patch); err != nil {
				return err
			}
			if len(patch) == 0 {
				return output.Errorf("nothing to update — provide a field flag, --enabled or --auto-publish")
			}

			feed, err := client.UpdateFeed(cmd.Context(), args[0], patch)
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(feedUpdateResult{Feed: *feed})
			}
			renderFeedSaved(p, "Updated", feed)
			return nil
		},
	})
	addFeedFieldFlags(cmd)
	return cmd
}

func newFeedListCmd() *cobra.Command {
	return markAdmin(&cobra.Command{
		Use:   "list",
		Short: "List all blog feed sources (admin)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			feeds, err := client.Feeds(cmd.Context())
			if err != nil {
				return adminAPIError(err)
			}
			// Stable order for human + golden output; the API returns newest-first.
			sort.Slice(feeds, func(i, j int) bool { return feeds[i].Label < feeds[j].Label })
			if p.Mode.JSON {
				return p.EmitJSON(feedListResult{Count: len(feeds), Feeds: feeds})
			}
			renderFeedTable(p, feeds)
			return nil
		},
	})
}

func newFeedSyncCmd() *cobra.Command {
	return markAdmin(&cobra.Command{
		Use:   "sync <id>",
		Short: "Ingest one blog feed source now (admin)",
		Long: "Fetch and mirror a single feed's current items immediately instead of waiting for the " +
			"scheduled ingest, and report how many items were fetched, created, updated and skipped. " +
			"A fetch/parse failure is a clear error — the feed was unreachable or unparseable; " +
			"per-item failures are surfaced as skipped items with their reasons." + adminNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			summary, err := client.SyncFeed(cmd.Context(), args[0])
			if err != nil {
				return adminAPIError(err)
			}
			// A non-empty Error is the server's 502: the fetch/parse failed.
			if summary.Error != "" {
				return output.Errorf("feed sync failed: %s", summary.Error)
			}
			if p.Mode.JSON {
				return p.EmitJSON(feedSyncResult{Summary: *summary})
			}
			renderFeedSync(p, summary)
			return nil
		},
	})
}

// renderFeedSaved prints the human one-liner for a created/updated feed.
func renderFeedSaved(p *output.Printer, verb string, feed *api.FeedSource) {
	p.Human("%s feed %q (kind %s, category %s, %s, id %s)\n",
		verb, feed.Label, feed.Kind, feed.DefaultCategory, feedState(feed.Enabled), feed.ID)
}

// renderFeedTable renders the feed list as a table; the last status is the
// fail-loud operational signal (— until the first sync).
func renderFeedTable(p *output.Printer, feeds []api.FeedSource) {
	rows := make([][]string, len(feeds))
	for i, f := range feeds {
		rows[i] = []string{f.Label, f.Kind, f.DefaultCategory, feedState(f.Enabled), deref(f.AuthorHandle), feedLastStatus(f)}
	}
	output.RenderTable(p.Out, []string{"LABEL", "KIND", "CATEGORY", "STATE", "AUTHOR", "LAST STATUS"}, rows)
}

// renderFeedSync prints the human summary of a sync; per-item errors ride
// Status (stderr) so the counts on stdout stay a clean data line.
func renderFeedSync(p *output.Printer, s *api.FeedSyncSummary) {
	p.Human("Synced feed %q: fetched %d, created %d, updated %d, skipped %d\n",
		s.Label, s.Fetched, s.Created, s.Updated, s.Skipped)
	for _, ie := range s.ItemErrors {
		p.Status("  skipped item: %s\n", ie)
	}
}

// feedState renders the on/off switch as a word for human output.
func feedState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "paused"
}

// feedLastStatus renders the last ingest status, mapping the never-synced case
// to a dash.
func feedLastStatus(f api.FeedSource) string {
	if f.LastStatus == nil || *f.LastStatus == "" {
		return "—"
	}
	return *f.LastStatus
}
