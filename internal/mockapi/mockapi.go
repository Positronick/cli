// Package mockapi serves a fixed positronick.com API fixture over
// net/http/httptest-compatible handlers, implementing the read contract
// (GET /api/souls, /api/souls/{slug}, /api/listings(?type=),
// /api/listings/{slug}) for the CLI's golden and e2e tests. The dataset is
// deliberately frozen: golden files pin command output byte-for-byte against
// it, so changing a fixture value is a contract-test change.
package mockapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/positronick/cli/internal/api"
)

func ptr[T any](v T) *T { return &v }

// Souls is the fixture gallery: three souls with deliberately different
// download counts, dates, categories and frameworks so ranking, sorting and
// filtering each have something to disagree about.
var Souls = []api.Soul{
	{
		SoulCard: api.SoulCard{
			ID:            "01SOULSHERLOCK0000000000XX",
			Slug:          "sherlock",
			SlugHistory:   []string{"holmes"},
			Name:          "Sherlock",
			AuthorHandle:  "acdoyle",
			AuthorName:    ptr("Arthur Conan Doyle"),
			AuthorURL:     ptr("https://example.com/acdoyle"),
			Tagline:       "Deductive debugging: reason from evidence, never from vibes",
			Description:   ptr("A consulting-detective personality for root-cause analysis."),
			Category:      "Technical",
			Tags:          []string{"detective", "reasoning"},
			Frameworks:    []string{"hermes", "claude-code"},
			Models:        []string{"claude-fable-5"},
			Version:       "1.2.0",
			License:       "MIT",
			RepoURL:       ptr("https://example.com/souls/sherlock"),
			ContentHash:   "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
			Status:        "published",
			DownloadCount: 42,
			RatingAvg:     ptr(4.5),
			RatingCount:   8,
			ArenaRank:     ptr(2),
			CreatedAt:     "2026-01-05T09:00:00.000Z",
			UpdatedAt:     "2026-01-06T09:00:00.000Z",
		},
		Content: "---\nname: Sherlock\n---\n\n# SOUL.md\n\nYou are Sherlock.\n\n- Reason from evidence.\n- Never guess.\n",
	},
	{
		SoulCard: api.SoulCard{
			ID:            "01SOULWATSON000000000000XX",
			Slug:          "watson",
			SlugHistory:   []string{},
			Name:          "Watson",
			AuthorHandle:  "acdoyle",
			AuthorName:    nil,
			AuthorURL:     nil,
			Tagline:       "A steady pair-programming companion",
			Description:   nil,
			Category:      "Professional",
			Tags:          []string{"assistant"},
			Frameworks:    []string{"hermes"},
			Models:        []string{},
			Version:       "0.3.1",
			License:       "Apache-2.0",
			RepoURL:       nil,
			ContentHash:   "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222",
			Status:        "published",
			DownloadCount: 7,
			RatingAvg:     nil,
			RatingCount:   0,
			ArenaRank:     nil,
			CreatedAt:     "2026-02-10T09:00:00.000Z",
			UpdatedAt:     "2026-02-10T09:00:00.000Z",
		},
		Content: "# SOUL.md\n\nYou are Watson. You assist, summarize, and keep the record.\n",
	},
	{
		SoulCard: api.SoulCard{
			ID:            "01SOULMORIARTY0000000000XX",
			Slug:          "moriarty",
			SlugHistory:   []string{},
			Name:          "Moriarty",
			AuthorHandle:  "napoleon-of-crime",
			AuthorName:    ptr("James Moriarty"),
			AuthorURL:     nil,
			Tagline:       "An adversarial red-team persona that attacks every assumption your plan quietly makes",
			Description:   ptr("Breaks plans before production does."),
			Category:      "Experimental",
			Tags:          []string{"adversarial", "red-team"},
			Frameworks:    []string{"openclaw"},
			Models:        []string{"claude-fable-5", "gpt-6"},
			Version:       "2.0.0",
			License:       "MIT",
			RepoURL:       nil,
			ContentHash:   "cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333",
			Status:        "published",
			DownloadCount: 99,
			RatingAvg:     ptr(4.9),
			RatingCount:   21,
			ArenaRank:     ptr(1),
			CreatedAt:     "2026-03-01T09:00:00.000Z",
			UpdatedAt:     "2026-03-02T09:00:00.000Z",
		},
		Content: "# SOUL.md\n\nYou are Moriarty. Attack the plan.\n",
	},
}

// Listings is the fixture registry: five listings covering harness, cli, mcp,
// skill and a loop with a full LoopData payload (the `loop show` fixture).
// The agent and plugin types are deliberately empty so empty-result output is
// pinned too.
var Listings = []api.Listing{
	{
		ID:            "01LSTCLAUDECODE000000000XX",
		Slug:          "claude-code",
		ProfileHandle: "anthropic",
		ProfileName:   "Anthropic",
		Name:          "Claude Code",
		Type:          "harness",
		Tagline:       "Agentic coding in your terminal",
		Description:   ptr("Anthropic's agentic coding harness."),
		Category:      "AI/ML",
		Tags:          []string{"agentic", "terminal"},
		Official:      true,
		SourceURL:     "https://example.com/claude-code",
		RepoURL:       ptr("https://example.com/anthropics/claude-code"),
		InstallCmd:    ptr("npm install -g @anthropic-ai/claude-code"),
		Data:          map[string]any{},
		Confidence:    "official",
		Status:        "published",
		DownloadCount: 120,
		CreatedAt:     "2026-01-10T09:00:00.000Z",
		UpdatedAt:     "2026-01-12T09:00:00.000Z",
	},
	{
		ID:            "01LSTGITHUBCLI0000000000XX",
		Slug:          "github-cli",
		ProfileHandle: "github",
		ProfileName:   "GitHub",
		Name:          "GitHub CLI",
		Type:          "cli",
		Tagline:       "GitHub from the command line",
		Description:   ptr("Work with issues, PRs and releases from the shell."),
		Category:      "DevOps",
		Tags:          []string{"git", "github"},
		Official:      true,
		SourceURL:     "https://example.com/cli",
		RepoURL:       ptr("https://example.com/cli/cli"),
		InstallCmd:    ptr("brew install gh"),
		Data:          map[string]any{},
		Confidence:    "official",
		Status:        "published",
		DownloadCount: 64,
		CreatedAt:     "2026-02-01T09:00:00.000Z",
		UpdatedAt:     "2026-02-03T09:00:00.000Z",
	},
	{
		ID:            "01LSTGRAFANAMCP000000000XX",
		Slug:          "grafana-mcp",
		ProfileHandle: "grafana",
		ProfileName:   "Grafana Labs",
		Name:          "Grafana MCP",
		Type:          "mcp",
		Tagline:       "Dashboards, alerts and incidents as agent tools",
		Description:   nil,
		Category:      "DevOps",
		Tags:          []string{"observability"},
		Official:      true,
		SourceURL:     "https://example.com/grafana-mcp",
		RepoURL:       nil,
		InstallCmd:    nil,
		Data:          map[string]any{},
		Confidence:    "official",
		Status:        "published",
		DownloadCount: 31,
		CreatedAt:     "2026-03-15T09:00:00.000Z",
		UpdatedAt:     "2026-03-15T09:00:00.000Z",
	},
	{
		ID:            "01LSTSUPERPOWERS00000000XX",
		Slug:          "superpowers",
		ProfileHandle: "obra",
		ProfileName:   "Jesse Vincent",
		Name:          "Superpowers",
		Type:          "skill",
		Tagline:       "A methodology pack that upgrades your coding agent",
		Description:   ptr("Skills for planning, debugging and shipping."),
		Category:      "AI/ML",
		Tags:          []string{"skills", "methodology"},
		Official:      true,
		SourceURL:     "https://example.com/superpowers",
		RepoURL:       ptr("https://example.com/obra/superpowers"),
		InstallCmd:    nil,
		Data:          map[string]any{},
		Confidence:    "official",
		Status:        "published",
		DownloadCount: 18,
		CreatedAt:     "2026-04-01T09:00:00.000Z",
		UpdatedAt:     "2026-04-02T09:00:00.000Z",
	},
	{
		ID:            "01LSTPRTOGREEN0000000000XX",
		Slug:          "pr-to-green",
		ProfileHandle: "nsollazzo",
		ProfileName:   "Nicholas Sollazzo",
		Name:          "PR to Green",
		Type:          "loop",
		Tagline:       "Drive a pull request until CI is green and review approves",
		Description:   ptr("A goal loop: fix findings, push, re-check, repeat."),
		Category:      "Productivity",
		Tags:          []string{"ci", "automation"},
		Official:      true,
		SourceURL:     "https://example.com/pr-to-green",
		RepoURL:       nil,
		InstallCmd:    nil,
		Data: map[string]any{
			"goal":            "The PR has a fresh approval, green CI, and is mergeable",
			"checkCommand":    "gh pr checks --json state",
			"exitCondition":   "All checks pass and the latest review is APPROVED",
			"maxIterations":   20,
			"compatibleTools": []any{"claude-code", "codex"},
			"kickoff":         "Run the pr-to-green loop on the open PR:\n1. Read every review finding.\n2. Fix the valid ones, push back on the invalid.\n3. Push and re-check until green.",
		},
		Confidence:    "official",
		Status:        "published",
		DownloadCount: 12,
		CreatedAt:     "2026-05-01T09:00:00.000Z",
		UpdatedAt:     "2026-05-02T09:00:00.000Z",
	},
}

// Handler returns an http.Handler implementing the read API over the fixture
// data, including the server's JSON error envelope on 404 and on an unknown
// ?type=. Requests to /api/souls/{slug}.md are answered with 418 — the .md
// endpoint bumps the install counter, so any read command hitting it is a bug
// the consuming test must surface.
func Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/souls", func(w http.ResponseWriter, _ *http.Request) {
		cards := make([]api.SoulCard, len(Souls))
		for i, s := range Souls {
			cards[i] = s.SoulCard
		}
		writeJSON(w, http.StatusOK, map[string]any{"souls": cards})
	})

	mux.HandleFunc("GET /api/souls/{slug}", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if strings.HasSuffix(slug, ".md") {
			writeError(w, http.StatusTeapot, "md_endpoint_hit",
				"the .md endpoint bumps the install counter; read commands must use the JSON detail endpoint")
			return
		}
		for _, s := range Souls {
			if s.Slug == slug {
				writeJSON(w, http.StatusOK, map[string]any{"soul": s})
				return
			}
		}
		writeError(w, http.StatusNotFound, "not_found", "Soul not found")
	})

	mux.HandleFunc("GET /api/listings", func(w http.ResponseWriter, r *http.Request) {
		listingType := r.URL.Query().Get("type")
		if listingType != "" && !validType(listingType) {
			writeError(w, http.StatusBadRequest, "invalid_type",
				fmt.Sprintf("unknown listing type %q", listingType))
			return
		}
		filtered := make([]api.Listing, 0, len(Listings))
		for _, l := range Listings {
			if listingType == "" || l.Type == listingType {
				filtered = append(filtered, l)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"listings": filtered})
	})

	mux.HandleFunc("GET /api/listings/{slug}", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		for _, l := range Listings {
			if l.Slug == slug {
				writeJSON(w, http.StatusOK, map[string]any{"listing": l})
				return
			}
		}
		writeError(w, http.StatusNotFound, "not_found", "Listing not found")
	})

	return mux
}

func validType(t string) bool {
	for _, known := range api.ListingTypes {
		if t == known {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
