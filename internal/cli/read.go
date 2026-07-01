package cli

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	// defaultLimit caps search/list results unless --limit overrides it.
	defaultLimit = 20
	// taglineWidth is the rune budget for the table's TAGLINE column, keeping
	// NAME + SLUG + TAGLINE comfortably inside a 100-column terminal.
	taglineWidth = 60
)

// sortKeys are the accepted --sort values. "relevance" is the website's
// ranking (fuzzy score desc, name asc on ties); the others re-order the
// matched set: name asc, downloads desc, newest (createdAt desc), each with
// name as the tiebreak.
var sortKeys = []string{"relevance", "name", "downloads", "newest"}

// readFlags carries the parsed, validated search/list flags.
type readFlags struct {
	limit     int
	sort      string
	category  string
	framework string
}

// addReadFlags registers the search/list flag set. Search defaults to
// relevance order, list to name order; only souls have a framework facet.
func addReadFlags(cmd *cobra.Command, defaultSort string, withFramework bool) {
	f := cmd.Flags()
	f.Int("limit", defaultLimit, "maximum number of results")
	f.String("sort", defaultSort, "sort order: relevance, name, downloads or newest")
	f.String("category", "", "only results in this category (case-insensitive)")
	if withFramework {
		f.String("framework", "", "only souls supporting this framework (case-insensitive)")
	}
}

// parseReadFlags reads and validates the flags registered by addReadFlags,
// failing loud — before any network call — on out-of-range values.
func parseReadFlags(cmd *cobra.Command, withFramework bool) (readFlags, error) {
	var rf readFlags
	var err error
	if rf.limit, err = cmd.Flags().GetInt("limit"); err != nil {
		return rf, err
	}
	if rf.sort, err = cmd.Flags().GetString("sort"); err != nil {
		return rf, err
	}
	if rf.category, err = cmd.Flags().GetString("category"); err != nil {
		return rf, err
	}
	if withFramework {
		if rf.framework, err = cmd.Flags().GetString("framework"); err != nil {
			return rf, err
		}
	}
	if rf.limit < 1 {
		return rf, output.Errorf("--limit must be at least 1, got %d", rf.limit)
	}
	if !slices.Contains(sortKeys, rf.sort) {
		return rf, output.Errorf("invalid --sort %q (valid: %s)", rf.sort, strings.Join(sortKeys, ", "))
	}
	return rf, nil
}

// searchQuery joins the positional args into the query and rejects an empty
// one: searching for nothing is a mistake, `list` is the way to browse.
func searchQuery(noun string, args []string) (string, error) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return "", output.ErrorWithHint(
			noun+" search requires a query",
			"to browse everything, run: positronick "+noun+" list")
	}
	return query, nil
}

// reorder re-sorts ranked items for a non-relevance --sort key, breaking ties
// by name ascending (case-insensitive) so output is deterministic. createdAt
// values are ISO-8601 strings (see internal/api/types.go), so plain string
// comparison is chronological.
func reorder[T any](items []T, sortKey string, name func(T) string, downloads func(T) int, createdAt func(T) string) {
	if sortKey == "relevance" {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		switch sortKey {
		case "downloads":
			if d := downloads(items[i]) - downloads(items[j]); d != 0 {
				return d > 0
			}
		case "newest":
			if a, b := createdAt(items[i]), createdAt(items[j]); a != b {
				return a > b
			}
		}
		a, b := strings.ToLower(name(items[i])), strings.ToLower(name(items[j]))
		return a < b
	})
}

// applyLimit truncates items to at most limit entries, after ranking/sorting.
func applyLimit[T any](items []T, limit int) []T {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

// notFoundWithSuggestion builds the exit-3 error for a slug the detail endpoint
// 404'd on, shared by `soul show` and `listing show`. When the catalog list
// holds a plausible neighbor it appends a did-you-mean hint; when the list
// holds the exact slug, the detail miss means the server is older than this CLI
// (no JSON detail endpoint yet), so it says that instead of suggesting the input
// back. A failing catalog fetch (fetchErr != nil) is ignored so it never masks
// the original not-found — the caller passes the fetch result verbatim.
func notFoundWithSuggestion(noun, slug string, catalog []string, fetchErr error) error {
	hint := fmt.Sprintf("Run: positronick %s list", noun)
	if fetchErr == nil {
		if match := closestSlug(slug, catalog); match == slug {
			hint = "the server lists this " + noun + " but could not return its details — positronick.com may be running an older API"
		} else if match != "" {
			hint = fmt.Sprintf("did you mean %q? %s", match, hint)
		}
	}
	return output.NotFoundError(fmt.Sprintf("%s %q not found", noun, slug), hint)
}

// truncateCell shortens s to at most width runes for table cells, marking the
// cut with an ellipsis.
func truncateCell(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
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

// fieldRows builds RenderFields rows from label/value pairs, dropping rows
// whose value is empty so human detail output never shows blank fields.
func fieldRows(pairs ...string) [][2]string {
	rows := make([][2]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			continue
		}
		rows = append(rows, [2]string{pairs[i], pairs[i+1]})
	}
	return rows
}

// deref renders an optional string field, mapping nil to "".
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// handleLabel renders "@handle (Full Name)", or just "@handle" without a name.
func handleLabel(handle, name string) string {
	if name == "" {
		return "@" + handle
	}
	return fmt.Sprintf("@%s (%s)", handle, name)
}
