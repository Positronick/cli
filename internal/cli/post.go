package cli

import (
	"context"
	"os"
	"sort"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// This file owns the hidden admin `post` command group (create/update/list)
// against /api/admin/posts — the blog "agent write API". Like the other admin
// commands they are Hidden + annotated (revealed for cached admins) and map a
// 401/403 to exit 4 via adminAPIError. Two things are deliberate and matter:
// create defaults status to "draft" (the server's default — an agent post must
// be promoted to publish, never self-publishes), and there is no delete verb
// (unpublish is `--status draft`). Ownership mirrors souls/listings but with
// feed semantics: editing a feed-mirrored post takes "api" ownership so the
// ingestor stops refreshing it; `--source feed` hands it back.

// postCreateResult is the `post create --json` contract.
type postCreateResult struct {
	Post    api.AdminPost `json:"post"`
	Created bool          `json:"created"`
}

// postUpdateResult is the `post update --json` contract — the server's response
// shape, tookOwnership included so agents can react to the feed→api flip.
type postUpdateResult struct {
	Post          api.AdminPost `json:"post"`
	TookOwnership bool          `json:"tookOwnership"`
}

// postListResult is the `post list --json` contract.
type postListResult struct {
	Count int             `json:"count"`
	Posts []api.AdminPost `json:"posts"`
}

// postFieldFlagMap maps the post string-field flags to their API field names
// (the repeatable --tag is handled separately).
var postFieldFlagMap = [][2]string{
	{"slug", "slug"},
	{"title", "title"},
	{"excerpt", "excerpt"},
	{"description", "description"},
	{"category", "category"},
	{"kind", "kind"},
	{"version", "version"},
	{"author-handle", "authorHandle"},
	{"listing-slug", "listingSlug"},
	{"canonical-url", "canonicalUrl"},
	{"published-at", "publishedAt"},
}

func newPostCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "post",
		Short: "Create, update and list blog posts (admin)",
		Long: "Write access to the positronick.com blog — the agent write API. Create a post from a " +
			"markdown file, patch one in place, or list every post (any status). New posts are drafts " +
			"unless you set --status: an agent post never self-publishes, it must be promoted " +
			"deliberately. There is no delete verb — unpublish with --status draft. Posts mirrored from " +
			"a feed source are feed-owned; editing one takes api ownership so the ingestor stops " +
			"refreshing it (hand it back with --source feed)." + adminNote,
	})
	cmd.AddCommand(newPostCreateCmd(), newPostUpdateCmd(), newPostListCmd())
	return cmd
}

// addPostFieldFlags registers the per-field override flags shared by post
// create and post update.
func addPostFieldFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("slug", "", "url slug — /blog/<slug> (overrides frontmatter)")
	f.String("title", "", "post title (overrides frontmatter)")
	f.String("excerpt", "", "short summary for cards and the RSS feed (overrides frontmatter)")
	f.String("description", "", "longer SEO description (overrides frontmatter)")
	f.String("category", "", "post category (overrides frontmatter)")
	f.String("kind", "", "post kind: article, release or link (overrides frontmatter)")
	f.String("version", "", "semver version (overrides frontmatter)")
	f.String("author-handle", "", "author profile handle (overrides frontmatter)")
	f.String("listing-slug", "", "related registry listing slug (overrides frontmatter)")
	f.String("canonical-url", "", "canonical backlink to the original release/item (overrides frontmatter)")
	f.String("published-at", "", "publish instant, ISO-8601 (overrides frontmatter)")
	f.StringArray("tag", nil, "post tag, repeatable (overrides frontmatter)")
}

// applyPostFieldFlags copies every explicitly set field flag over fields —
// flags always beat frontmatter.
func applyPostFieldFlags(cmd *cobra.Command, fields map[string]any) error {
	for _, pair := range postFieldFlagMap {
		if !cmd.Flags().Changed(pair[0]) {
			continue
		}
		v, err := cmd.Flags().GetString(pair[0])
		if err != nil {
			return err
		}
		fields[pair[1]] = v
	}
	if cmd.Flags().Changed("tag") {
		v, err := cmd.Flags().GetStringArray("tag")
		if err != nil {
			return err
		}
		fields["tags"] = v
	}
	return nil
}

// postServerAssigned are the read-only/derived post fields stripped from a
// --file's frontmatter so a post copied straight out of the blog content
// source just works: ids and timestamps are server-assigned, the content hash
// is derived from the body, the view count is runtime, and author*/listingName
// are denormalized server-side from authorHandle/listingSlug.
var postServerAssigned = []string{
	"id", "createdAt", "updatedAt", "contentHash", "viewCount",
	"authorName", "authorAvatar", "authorTier", "listingName",
}

// parsePostFile parses a post markdown file into the admin-API field map: the
// YAML frontmatter keys verbatim, minus the server-assigned/derived fields,
// plus the body as "content". Unknown keys are passed through on purpose — the
// server's validator is the single authority and its 422 is surfaced verbatim.
// It mirrors parseSoulFile over the shared splitFrontmatter, with the post
// drop set.
func parsePostFile(path, raw string) (map[string]any, error) {
	yamlText, body, err := splitFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if yamlText != "" {
		if err := yaml.Unmarshal([]byte(yamlText), &fields); err != nil {
			return nil, output.Errorf("parsing frontmatter in %s: %v", path, err)
		}
	}
	for _, key := range postServerAssigned {
		delete(fields, key)
	}
	fields["content"] = body
	return fields, nil
}

// postFieldsFromFile reads and parses the --file markdown into the field map.
func postFieldsFromFile(cmd *cobra.Command) (map[string]any, error) {
	path, err := cmd.Flags().GetString("file")
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, output.Errorf("reading %s: %v", path, err)
	}
	return parsePostFile(path, string(raw))
}

func newPostCreateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "create --file POST.md",
		Short: "Create a blog post on positronick.com (admin)",
		Long: "Create a post from a markdown file (YAML frontmatter + markdown body). Required " +
			"frontmatter: slug, title, excerpt, category; the body becomes the post content. Field " +
			"flags override frontmatter. The post is a DRAFT unless --status says otherwise — an " +
			"agent post does not self-publish. Any id/createdAt/updatedAt/contentHash in the " +
			"frontmatter is ignored (the server assigns them) and the post is api-owned, so the feed " +
			"ingestor never touches it. Validation is the server's; a 422 carries its message " +
			"verbatim." + adminNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			fields, err := postFieldsFromFile(cmd)
			if err != nil {
				return err
			}
			if err := applyPostFieldFlags(cmd, fields); err != nil {
				return err
			}
			if err := applyStringFlag(cmd, "status", fields); err != nil {
				return err
			}

			post, err := client.CreatePost(cmd.Context(), fields)
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(postCreateResult{Post: *post, Created: true})
			}
			p.Human("Created post %s (id %s, status %s)\n", post.Slug, post.ID, post.Status)
			p.Human("Public path: /blog/%s\n", post.Slug)
			if post.Status == "draft" {
				p.Status("hint: still a draft — publish with `positronick post update %s --status published`\n", post.ID)
			}
			return nil
		},
	})
	cmd.Flags().String("file", "", "path to the post markdown file (YAML frontmatter + markdown body)")
	_ = cmd.MarkFlagRequired("file")
	addPostFieldFlags(cmd)
	cmd.Flags().String("status", "", "publication status: draft, pending or published (default: draft)")
	return cmd
}

func newPostUpdateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "update <id-or-slug>",
		Short: "Update a blog post on positronick.com (admin)",
		Long: "Patch a post in place: only the fields you provide change. --file replaces the " +
			"frontmatter fields and the whole markdown body; field flags override either way. " +
			"Unpublish with --status draft (there is no delete verb). The argument is the post id, or " +
			"a published slug (drafts are only addressable by id). Editing a feed-mirrored post takes " +
			"api ownership — the ingestor stops refreshing it; pass --source feed to hand it back." +
			adminNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			patch := map[string]any{}
			if cmd.Flags().Changed("file") {
				fileFields, err := postFieldsFromFile(cmd)
				if err != nil {
					return err
				}
				for k, v := range fileFields {
					patch[k] = v
				}
			}
			if err := applyPostFieldFlags(cmd, patch); err != nil {
				return err
			}
			for _, flag := range []string{"status", "source"} {
				if err := applyStringFlag(cmd, flag, patch); err != nil {
					return err
				}
			}
			if len(patch) == 0 {
				return output.Errorf("nothing to update — provide --file, field flags, --status or --source")
			}

			ref := args[0]
			post, took, err := client.UpdatePost(cmd.Context(), ref, patch)
			if api.IsNotFound(err) && !isULID(ref) {
				// Not an id and not shaped like one: resolve it as a slug via the
				// public blog detail endpoint and retry.
				var id string
				if id, err = postIDForSlug(cmd.Context(), client, ref); err != nil {
					return err
				}
				post, took, err = client.UpdatePost(cmd.Context(), id, patch)
			}
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(postUpdateResult{Post: *post, TookOwnership: took})
			}
			p.Human("Updated post %s (id %s, status %s)\n", post.Slug, post.ID, post.Status)
			postOwnershipWarning(p, took)
			return nil
		},
	})
	cmd.Flags().String("file", "", "post markdown replacing the frontmatter fields and the whole body")
	addPostFieldFlags(cmd)
	cmd.Flags().String("status", "", "publication status: draft (= unpublish), pending or published")
	cmd.Flags().String("source", "", "ownership: api (default on any change) or feed (hand back to the ingestor)")
	return cmd
}

func newPostListCmd() *cobra.Command {
	return markAdmin(&cobra.Command{
		Use:   "list",
		Short: "List all blog posts, any status (admin)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			posts, err := client.AdminPosts(cmd.Context())
			if err != nil {
				return adminAPIError(err)
			}
			// Stable order for human + golden output; the API returns newest-first.
			sort.Slice(posts, func(i, j int) bool { return posts[i].Slug < posts[j].Slug })
			if p.Mode.JSON {
				return p.EmitJSON(postListResult{Count: len(posts), Posts: posts})
			}
			renderPostTable(p, posts)
			return nil
		},
	})
}

// postIDForSlug resolves a published slug to its post id via the public blog
// detail endpoint.
func postIDForSlug(ctx context.Context, client *api.Client, slug string) (string, error) {
	post, err := client.Post(ctx, slug)
	if api.IsNotFound(err) {
		return "", output.NotFoundError("post \""+slug+"\" not found",
			"pass the post id — drafts are only addressable by id. Run: positronick post list")
	}
	if err != nil {
		return "", err
	}
	return post.ID, nil
}

// postOwnershipWarning prints the feed→api flip warning. It rides Status
// (stderr, suppressed in JSON mode where the tookOwnership field says the same
// thing). Posts flip from the feed ingestor, not the git seed, so the wording
// differs from ownershipWarning.
func postOwnershipWarning(p *output.Printer, took bool) {
	if !took {
		return
	}
	p.Status("WARNING: took ownership from the feed — the ingestor will no longer refresh this post " +
		"from its source (update with --source feed to hand it back).\n")
}

func renderPostTable(p *output.Printer, posts []api.AdminPost) {
	rows := make([][]string, len(posts))
	for i, post := range posts {
		rows[i] = []string{post.Slug, post.Kind, post.Status, post.Source, deref(post.AuthorHandle), truncateCell(post.Title, taglineWidth)}
	}
	output.RenderTable(p.Out, []string{"SLUG", "KIND", "STATUS", "SOURCE", "AUTHOR", "TITLE"}, rows)
}
