package cli

import (
	"sort"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// This file owns the hidden admin `feed` command group (list/create/update/sync)
// against /api/admin/feeds. Feed sources drive the blog's release/RSS ingest —
// GitHub releases use kind=github_release with feedUrl=the repo URL; RSS uses
// the feed URL. Like the other admin commands they are Hidden + annotated
// (revealed for cached admins) and map a 401/403 to exit 4 via adminAPIError.
// There is no delete verb: pause with `feed update --disabled`.

// feedAdminNote is the Long-help tail for feed commands. Feeds have no
// --status draft unpublish path (adminNote would mislead), so they get their
// own note pointing at --disabled.
const feedAdminNote = "\n\nRequires an admin account (positronick login). Ids are server-assigned " +
	"ULIDs — never supply one. There is no delete verb: pause with feed update --disabled."

// feedListResult is the `feed list --json` contract.
type feedListResult struct {
	Count int             `json:"count"`
	Feeds []api.AdminFeed `json:"feeds"`
}

// feedCreateResult is the `feed create --json` contract.
type feedCreateResult struct {
	Feed    api.AdminFeed `json:"feed"`
	Created bool          `json:"created"`
}

// feedUpdateResult is the `feed update --json` contract.
type feedUpdateResult struct {
	Feed api.AdminFeed `json:"feed"`
}

// feedSyncResult is the `feed sync --json` contract.
type feedSyncResult struct {
	Summary api.FeedSyncSummary `json:"summary"`
}

func newFeedCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "feed",
		Short: "Subscribe and manage release/RSS feed sources (admin)",
		Long: "Manage feed sources the blog ingestor mirrors into release/link posts. " +
			"GitHub releases use kind=github_release with feedUrl=the repo URL; RSS uses the feed URL. " +
			"No delete verb — pause with `feed update --disabled`." + feedAdminNote,
	})
	cmd.AddCommand(
		newFeedListCmd(),
		newFeedCreateCmd(),
		newFeedUpdateCmd(),
		newFeedSyncCmd(),
	)
	return cmd
}

func newFeedListCmd() *cobra.Command {
	return markAdmin(&cobra.Command{
		Use:   "list",
		Short: "List all feed sources (admin)",
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
			// Stable order for human + golden output; the API returns DB order.
			sort.Slice(feeds, func(i, j int) bool { return feeds[i].Label < feeds[j].Label })
			if p.Mode.JSON {
				return p.EmitJSON(feedListResult{Count: len(feeds), Feeds: feeds})
			}
			renderFeedTable(p, feeds)
			return nil
		},
	})
}

func newFeedCreateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "create --label LABEL --url URL",
		Short: "Subscribe a release/RSS feed source (admin)",
		Long: "Create a feed source the blog ingestor will mirror. For GitHub releases, " +
			"--url is the repository URL and --kind defaults to github_release. --author " +
			"and --listing must already exist (same attribution rule as listings). " +
			"--auto-publish defaults false; --disabled starts the feed paused." + feedAdminNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			fields, err := feedFieldsFromFlags(cmd, false)
			if err != nil {
				return err
			}
			feed, err := client.CreateFeed(cmd.Context(), fields)
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(feedCreateResult{Feed: *feed, Created: true})
			}
			p.Human("Created feed %s (%s, id %s) — listing=%s author=%s autoPublish=%v enabled=%v\n",
				feed.Label, feed.Kind, feed.ID,
				deref(feed.ListingSlug), deref(feed.AuthorHandle),
				feed.AutoPublish, feed.Enabled)
			return nil
		},
	})
	addFeedFieldFlags(cmd, true)
	for _, required := range []string{"label", "url"} {
		_ = cmd.MarkFlagRequired(required)
	}
	return cmd
}

func newFeedUpdateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "update <id>",
		Short: "Update a feed source (admin)",
		Long: "Patch a feed source in place: only the flags you provide change. Empty " +
			"--author or --listing clears the attribution. --enabled / --disabled are " +
			"mutually exclusive; there is no delete verb — pause with --disabled." + feedAdminNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			patch, err := feedFieldsFromFlags(cmd, true)
			if err != nil {
				return err
			}
			if len(patch) == 0 {
				return output.Errorf("nothing to update — provide field flags, --enabled or --disabled")
			}
			feed, err := client.UpdateFeed(cmd.Context(), args[0], patch)
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(feedUpdateResult{Feed: *feed})
			}
			p.Human("Updated feed %s (id %s, enabled=%v, autoPublish=%v)\n",
				feed.Label, feed.ID, feed.Enabled, feed.AutoPublish)
			return nil
		},
	})
	addFeedFieldFlags(cmd, false)
	return cmd
}

func newFeedSyncCmd() *cobra.Command {
	return markAdmin(&cobra.Command{
		Use:   "sync <id>",
		Short: "Ingest one feed now (admin)",
		Long: "Run the blog ingestor against a single feed source immediately and print " +
			"the sync summary (fetched/created/updated/skipped). A failed fetch surfaces " +
			"as a server error; there is no offline dry-run." + feedAdminNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			sum, err := client.SyncFeed(cmd.Context(), args[0])
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(feedSyncResult{Summary: *sum})
			}
			p.Human("Synced feed %s (id %s): fetched=%d created=%d updated=%d skipped=%d\n",
				sum.Label, sum.FeedID, sum.Fetched, sum.Created, sum.Updated, sum.Skipped)
			if len(sum.ItemErrors) > 0 {
				p.Human("Item errors: %d\n", len(sum.ItemErrors))
			}
			return nil
		},
	})
}

// addFeedFieldFlags registers the shared create/update field flags. create=true
// sets defaults (kind/category) and only --disabled; update adds --enabled and
// makes the pair mutually exclusive.
func addFeedFieldFlags(cmd *cobra.Command, create bool) {
	f := cmd.Flags()
	f.String("label", "", "display label for the feed source")
	f.String("url", "", "repository URL (github_release) or feed URL (rss)")
	kindDefault, categoryDefault := "", ""
	if create {
		kindDefault, categoryDefault = "github_release", "Releases"
	}
	f.String("kind", kindDefault, "feed kind: github_release or rss")
	f.String("category", categoryDefault, "default blog category for ingested posts")
	f.String("author", "", "authoring profile handle (must exist); empty on update clears")
	f.String("listing", "", "linked listing slug (must exist); empty on update clears")
	f.StringArray("tag", nil, "default tag applied to ingested posts, repeatable")
	f.Bool("auto-publish", false, "auto-publish ingested posts (default false)")
	f.Bool("disabled", false, "create/update the feed as paused (enabled=false)")
	if !create {
		f.Bool("enabled", false, "re-enable a paused feed")
		cmd.MarkFlagsMutuallyExclusive("enabled", "disabled")
	}
}

// feedFieldsFromFlags builds the admin-API field map from explicitly set flags.
// partial=true (update) only includes Changed flags and allows empty
// author/listing to clear attribution; partial=false (create) always sends
// required/defaulted fields and omits empty optionals.
func feedFieldsFromFlags(cmd *cobra.Command, partial bool) (map[string]any, error) {
	fields := map[string]any{}

	setString := func(flag, key string, always, allowEmpty bool) error {
		if partial && !cmd.Flags().Changed(flag) {
			return nil
		}
		v, err := cmd.Flags().GetString(flag)
		if err != nil {
			return err
		}
		if !always && v == "" && !allowEmpty {
			return nil
		}
		// On update, empty author/listing is meaningful (clear); on create we
		// already skipped empties above unless always.
		if allowEmpty || v != "" || always {
			fields[key] = v
		}
		return nil
	}

	if err := setString("label", "label", !partial, false); err != nil {
		return nil, err
	}
	if err := setString("url", "feedUrl", !partial, false); err != nil {
		return nil, err
	}
	if err := setString("kind", "kind", !partial, false); err != nil {
		return nil, err
	}
	if err := setString("category", "defaultCategory", !partial, false); err != nil {
		return nil, err
	}
	// author/listing: on update, empty string clears; on create, omit if empty.
	if err := setString("author", "authorHandle", false, partial); err != nil {
		return nil, err
	}
	if err := setString("listing", "listingSlug", false, partial); err != nil {
		return nil, err
	}

	if !partial || cmd.Flags().Changed("tag") {
		tags, err := cmd.Flags().GetStringArray("tag")
		if err != nil {
			return nil, err
		}
		if tags == nil {
			tags = []string{}
		}
		// Always send on create (empty array is the server default); on update
		// only when --tag was set so we never wipe tags by accident.
		fields["defaultTags"] = tags
	}

	// On create always send so the wire body is explicit; on update only when
	// Changed (covers --auto-publish and --auto-publish=false).
	if !partial || cmd.Flags().Changed("auto-publish") {
		v, err := cmd.Flags().GetBool("auto-publish")
		if err != nil {
			return nil, err
		}
		fields["autoPublish"] = v
	}

	if cmd.Flags().Changed("disabled") {
		disabled, err := cmd.Flags().GetBool("disabled")
		if err != nil {
			return nil, err
		}
		// --disabled means enabled=false; --disabled=false on update re-enables.
		// On create without the flag we omit enabled so the server defaults true.
		fields["enabled"] = !disabled
	}

	if partial && cmd.Flags().Lookup("enabled") != nil && cmd.Flags().Changed("enabled") {
		enabled, err := cmd.Flags().GetBool("enabled")
		if err != nil {
			return nil, err
		}
		fields["enabled"] = enabled
	}

	return fields, nil
}

func renderFeedTable(p *output.Printer, feeds []api.AdminFeed) {
	rows := make([][]string, len(feeds))
	for i, f := range feeds {
		rows[i] = []string{
			f.ID,
			f.Label,
			f.Kind,
			deref(f.ListingSlug),
			deref(f.AuthorHandle),
			yn(f.AutoPublish),
			yn(f.Enabled),
			deref(f.LastStatus),
			f.FeedURL,
		}
	}
	output.RenderTable(p.Out, []string{"ID", "LABEL", "KIND", "LISTING", "AUTHOR", "AUTO", "ON", "LAST", "URL"}, rows)
}

func yn(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
