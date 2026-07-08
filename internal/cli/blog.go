package cli

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// This file owns the public `blog` command — read-only, unauthenticated access
// to positronick.com's published blog: editorial articles, mirrored GitHub
// releases, and mirrored news links. It mirrors the soul/listing reads:
// `blog list` (optionally filtered by --kind) and `blog show <slug>` with a
// --raw flag that prints the markdown body verbatim. Like the souls gallery,
// the server returns posts newest-first.

// blogListResult is the `blog list --json` contract: {"count":N,"posts":[...]}.
type blogListResult struct {
	Count int            `json:"count"`
	Posts []api.PostCard `json:"posts"`
}

// blogDetail is the `blog show --json` contract: {"post":{...}}.
type blogDetail struct {
	Post api.Post `json:"post"`
}

func newBlogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blog",
		Short: "Read the positronick.com blog (articles, releases, links)",
	}
	cmd.AddCommand(newBlogListCmd(), newBlogShowCmd())
	return cmd
}

func newBlogListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List published blog posts, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kind, err := cmd.Flags().GetString("kind")
			if err != nil {
				return err
			}
			// Validate before any network call, naming the valid set (the server
			// would 400, but failing loud locally is the agent-friendlier path).
			if kind != "" && !slices.Contains(api.PostKinds, kind) {
				return output.Errorf("invalid --kind %q (valid: %s)", kind, strings.Join(api.PostKinds, ", "))
			}
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			client, err := clientFor(cmd)
			if err != nil {
				return err
			}
			posts, err := client.Posts(cmd.Context(), kind)
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(blogListResult{Count: len(posts), Posts: posts})
			}
			renderBlogTable(p, posts)
			return nil
		},
	}
	cmd.Flags().String("kind", "", "only this post kind: article, release or link")
	return cmd
}

func renderBlogTable(p *output.Printer, posts []api.PostCard) {
	rows := make([][]string, len(posts))
	for i, post := range posts {
		rows[i] = []string{researchDate(post.PublishedAt), post.Kind, truncateCell(post.Title, researchTitleWidth), post.Slug}
	}
	output.RenderTable(p.Out, []string{"PUBLISHED", "KIND", "TITLE", "SLUG"}, rows)
}

func newBlogShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <slug>",
		Short: "Show one blog post in full",
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
			// --raw reads the .md endpoint: for blog posts it never bumps a
			// counter (unlike soul .md, which is why soul --raw uses the JSON
			// body), so it is the natural source of the verbatim markdown.
			if raw {
				md, err := client.PostMarkdown(cmd.Context(), slug)
				if api.IsNotFound(err) {
					return blogNotFound(cmd.Context(), client, slug)
				}
				if err != nil {
					return err
				}
				p.Human("%s", md)
				return nil
			}

			post, err := client.Post(cmd.Context(), slug)
			if api.IsNotFound(err) {
				return blogNotFound(cmd.Context(), client, slug)
			}
			if err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(blogDetail{Post: *post})
			}
			renderBlogDetail(p, post)
			return nil
		},
	}
	cmd.Flags().Bool("raw", false, "print only the markdown body verbatim (overrides --json)")
	return cmd
}

// blogNotFound builds the exit-3 error for a missing slug, with a did-you-mean
// hint when the gallery has a plausible neighbor. A failing suggestion fetch
// never masks the original not-found. Mirrors soulNotFound.
func blogNotFound(ctx context.Context, client *api.Client, slug string) error {
	hint := "Run: positronick blog list"
	if posts, err := client.Posts(ctx, ""); err == nil {
		slugs := make([]string, len(posts))
		for i, post := range posts {
			slugs[i] = post.Slug
		}
		if match := closestSlug(slug, slugs); match == slug {
			// The list knows the slug but the detail fetch 404'd: an older server,
			// not a typo — say so rather than suggesting the input back.
			hint = "the server lists this post but could not return its details — positronick.com may be running an older API"
		} else if match != "" {
			hint = fmt.Sprintf("did you mean %q? %s", match, hint)
		}
	}
	return output.NotFoundError(fmt.Sprintf("post %q not found", slug), hint)
}

func renderBlogDetail(p *output.Printer, post *api.Post) {
	author := ""
	if post.AuthorHandle != nil {
		author = handleLabel(*post.AuthorHandle, deref(post.AuthorName))
	}
	listing := deref(post.ListingSlug)
	if post.ListingName != nil && listing != "" {
		listing = fmt.Sprintf("%s (%s)", *post.ListingName, listing)
	}
	output.RenderFields(p.Out, fieldRows(
		"TITLE", post.Title,
		"SLUG", post.Slug,
		"KIND", post.Kind,
		"AUTHOR", author,
		"EXCERPT", post.Excerpt,
		"DESCRIPTION", deref(post.Description),
		"CATEGORY", post.Category,
		"TAGS", strings.Join(post.Tags, ", "),
		"VERSION", post.Version,
		"LISTING", listing,
		"CANONICAL", deref(post.CanonicalURL),
		"STATUS", post.Status,
		"VIEWS", strconv.Itoa(post.ViewCount),
		"PUBLISHED", deref(post.PublishedAt),
		"CREATED", post.CreatedAt,
		"UPDATED", post.UpdatedAt,
	))
	p.Status("hint: run `positronick blog show %s --raw` to print the markdown body\n", post.Slug)
}
