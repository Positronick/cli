// Package mockapi serves a fixed positronick.com API fixture over
// net/http/httptest-compatible handlers, implementing the read contract
// (GET /api/souls, /api/souls/{slug}, /api/listings(?type=),
// /api/listings/{slug}, /api/research) and the auth contract (device flow,
// /api/me, api-key/create) for the CLI's golden and e2e tests. The dataset is
// deliberately frozen: golden files pin command output byte-for-byte against
// it, so changing a fixture value is a contract-test change.
package mockapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/positronick/cli/internal/api"
)

func ptr[T any](v T) *T { return &v }

// Auth fixture values. The device flow is scripted: the first token poll for
// DeviceCode answers authorization_pending, every later poll succeeds with
// AccessToken. The advertised poll interval is 0 so tests never really wait —
// interval pacing (and slow_down growth) is unit-tested in internal/auth.
const (
	// DeviceCode is the device_code issued by /api/auth/device/code.
	DeviceCode = "mock-device-code-0001"
	// UserCode is the user_code the person types on the verification page.
	UserCode = "TJSPLLAV"
	// AccessToken is the session token granted once the device is approved;
	// it is the only bearer /api/me and api-key/create accept.
	AccessToken = "mock-session-token"
	// APIKey is the raw key minted by api-key/create, also accepted by
	// /api/me as x-api-key.
	APIKey = "posi_mockkey1234567890"
)

// MeUser is the identity /api/me answers for valid credentials (isAdmin
// false).
var MeUser = api.User{
	ID:          "usr_mock0001",
	Name:        "Ada Lovelace",
	Email:       "ada@example.com",
	Image:       ptr("https://example.com/ada.png"),
	GithubLogin: ptr("ada"),
}

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
			ChargeCount:   5,
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
			ChargeCount:   0,
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
			ChargeCount:   11,
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
		ID:               "01LSTCLAUDECODE000000000XX",
		Slug:             "claude-code",
		ProfileHandle:    "anthropic",
		ProfileName:      "Anthropic",
		ProfileTier:      ptr("official"),
		Name:             "Claude Code",
		Type:             "harness",
		Tagline:          "Agentic coding in your terminal",
		Description:      ptr("Anthropic's agentic coding harness."),
		Category:         "AI/ML",
		Tags:             []string{"agentic", "terminal"},
		Official:         true,
		SourceURL:        "https://example.com/claude-code",
		RepoURL:          ptr("https://example.com/anthropics/claude-code"),
		InstallCmd:       ptr("npm install -g @anthropic-ai/claude-code"),
		Data:             map[string]any{},
		HasAsset:         false,
		AssetVersion:     nil,
		AssetContentHash: nil,
		Confidence:       "official",
		Status:           "published",
		DownloadCount:    120,
		ChargeCount:      9,
		CreatedAt:        "2026-01-10T09:00:00.000Z",
		UpdatedAt:        "2026-01-12T09:00:00.000Z",
	},
	{
		ID:               "01LSTGITHUBCLI0000000000XX",
		Slug:             "github-cli",
		ProfileHandle:    "github",
		ProfileName:      "GitHub",
		ProfileTier:      ptr("official"),
		Name:             "GitHub CLI",
		Type:             "cli",
		Tagline:          "GitHub from the command line",
		Description:      ptr("Work with issues, PRs and releases from the shell."),
		Category:         "DevOps",
		Tags:             []string{"git", "github"},
		Official:         true,
		SourceURL:        "https://example.com/cli",
		RepoURL:          ptr("https://example.com/cli/cli"),
		InstallCmd:       ptr("brew install gh"),
		Data:             map[string]any{},
		HasAsset:         false,
		AssetVersion:     nil,
		AssetContentHash: nil,
		Confidence:       "official",
		Status:           "published",
		DownloadCount:    64,
		ChargeCount:      4,
		CreatedAt:        "2026-02-01T09:00:00.000Z",
		UpdatedAt:        "2026-02-03T09:00:00.000Z",
	},
	{
		ID:               "01LSTGRAFANAMCP000000000XX",
		Slug:             "grafana-mcp",
		ProfileHandle:    "grafana",
		ProfileName:      "Grafana Labs",
		ProfileTier:      ptr("official"),
		Name:             "Grafana MCP",
		Type:             "mcp",
		Tagline:          "Dashboards, alerts and incidents as agent tools",
		Description:      nil,
		Category:         "DevOps",
		Tags:             []string{"observability"},
		Official:         true,
		SourceURL:        "https://example.com/grafana-mcp",
		RepoURL:          nil,
		InstallCmd:       nil,
		Data:             map[string]any{},
		HasAsset:         false,
		AssetVersion:     nil,
		AssetContentHash: nil,
		Confidence:       "official",
		Status:           "published",
		DownloadCount:    31,
		ChargeCount:      2,
		CreatedAt:        "2026-03-15T09:00:00.000Z",
		UpdatedAt:        "2026-03-15T09:00:00.000Z",
	},
	{
		ID:               "01LSTSUPERPOWERS00000000XX",
		Slug:             "superpowers",
		ProfileHandle:    "obra",
		ProfileName:      "Jesse Vincent",
		ProfileTier:      ptr("verified"),
		Name:             "Superpowers",
		Type:             "skill",
		Tagline:          "A methodology pack that upgrades your coding agent",
		Description:      ptr("Skills for planning, debugging and shipping."),
		Category:         "AI/ML",
		Tags:             []string{"skills", "methodology"},
		Official:         true,
		SourceURL:        "https://example.com/superpowers",
		RepoURL:          ptr("https://example.com/obra/superpowers"),
		InstallCmd:       nil,
		Data:             map[string]any{"bundles": []any{"pr-to-green"}},
		HasAsset:         true,
		AssetVersion:     ptr("1.0.0"),
		AssetContentHash: ptr("11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa"),
		Confidence:       "official",
		Status:           "published",
		DownloadCount:    18,
		ChargeCount:      6,
		CreatedAt:        "2026-04-01T09:00:00.000Z",
		UpdatedAt:        "2026-04-02T09:00:00.000Z",
	},
	{
		ID:            "01LSTPRTOGREEN0000000000XX",
		Slug:          "pr-to-green",
		ProfileHandle: "nsollazzo",
		ProfileName:   "Nicholas Sollazzo",
		ProfileTier:   ptr("official"),
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
			"bundles":         []any{"superpowers"},
		},
		HasAsset:         false,
		AssetVersion:     nil,
		AssetContentHash: nil,
		Confidence:       "official",
		Status:           "published",
		DownloadCount:    12,
		ChargeCount:      3,
		CreatedAt:        "2026-05-01T09:00:00.000Z",
		UpdatedAt:        "2026-05-02T09:00:00.000Z",
	},
}

// ResearchPosts is the fixture "what's new" feed: one of each post kind
// (article, release, link) with deliberately different publish dates,
// categories and tags so --since/--kind/--category/--tag and the `latest`
// high-water mark each have something to disagree about. Ordered oldest-first;
// the handler sorts newest-first like the server.
var ResearchPosts = []api.ResearchItem{
	{
		Slug:         "shipping-the-cli",
		Title:        "Shipping the Positronick CLI",
		Excerpt:      "Install souls and browse the registry from your terminal.",
		Kind:         "article",
		Category:     "Engineering",
		Tags:         []string{"cli", "launch"},
		URL:          "https://positronick.com/blog/shipping-the-cli",
		MdURL:        "https://positronick.com/api/blog/shipping-the-cli.md",
		CanonicalURL: nil,
		ContentHash:  "dddd4444dddd4444dddd4444dddd4444dddd4444dddd4444dddd4444dddd4444",
		PublishedAt:  ptr("2026-05-20T12:00:00.000Z"),
	},
	{
		Slug:         "hermes-v2-1-0",
		Title:        "Hermes v2.1.0",
		Excerpt:      "Tool-calling fixes and a faster device flow.",
		Kind:         "release",
		Category:     "Releases",
		Tags:         []string{"hermes"},
		URL:          "https://positronick.com/blog/hermes-v2-1-0",
		MdURL:        "https://positronick.com/api/blog/hermes-v2-1-0.md",
		CanonicalURL: ptr("https://github.com/NousResearch/hermes/releases/tag/v2.1.0"),
		ContentHash:  "eeee5555eeee5555eeee5555eeee5555eeee5555eeee5555eeee5555eeee5555",
		PublishedAt:  ptr("2026-06-01T09:00:00.000Z"),
	},
	{
		Slug:         "openclaw-launch",
		Title:        "OpenClaw launches its agent harness",
		Excerpt:      "A new open-source harness joins the registry.",
		Kind:         "link",
		Category:     "Community",
		Tags:         []string{"openclaw", "news"},
		URL:          "https://positronick.com/blog/openclaw-launch",
		MdURL:        "https://positronick.com/api/blog/openclaw-launch.md",
		CanonicalURL: ptr("https://example.com/openclaw-launch"),
		ContentHash:  "ffff6666ffff6666ffff6666ffff6666ffff6666ffff6666ffff6666ffff6666",
		PublishedAt:  ptr("2026-06-10T08:00:00.000Z"),
	},
}

// Handler returns an http.Handler implementing the read API over the fixture
// data, including the server's JSON error envelope on 404 and on an unknown
// ?type=. Requests to /api/souls/{slug}.md are answered with 418 — the .md
// endpoint bumps the install counter, so any read command hitting it is a bug
// the consuming test must surface.
func Handler() http.Handler {
	mux := http.NewServeMux()
	registerAdmin(mux) // the /api/admin write surface (see admin.go)

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

	mux.HandleFunc("GET /api/research", func(w http.ResponseWriter, r *http.Request) {
		serveResearch(w, r.URL.Query())
	})

	mux.HandleFunc("POST /api/auth/device/code", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientID string `json:"client_id"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		if body.ClientID != "positronick-cli" {
			writeOAuthError(w, "invalid_client", "unknown client_id")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"device_code":               DeviceCode,
			"user_code":                 UserCode,
			"verification_uri":          "http://" + r.Host + "/device",
			"verification_uri_complete": "http://" + r.Host + "/device?user_code=" + UserCode,
			"expires_in":                1800,
			"interval":                  0,
		})
	})

	var pollMu sync.Mutex
	polls := 0
	mux.HandleFunc("POST /api/auth/device/token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			GrantType  string `json:"grant_type"`
			DeviceCode string `json:"device_code"`
			ClientID   string `json:"client_id"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		if body.GrantType != "urn:ietf:params:oauth:grant-type:device_code" {
			writeOAuthError(w, "unsupported_grant_type", "")
			return
		}
		if body.DeviceCode != DeviceCode || body.ClientID != "positronick-cli" {
			writeOAuthError(w, "invalid_grant", "")
			return
		}
		pollMu.Lock()
		polls++
		pending := polls == 1
		pollMu.Unlock()
		if pending {
			writeOAuthError(w, "authorization_pending", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": AccessToken,
			"token_type":   "Bearer",
			"expires_in":   604799,
			"scope":        "",
		})
	})

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": MeUser, "isAdmin": false})
	})

	mux.HandleFunc("POST /api/auth/api-key/create", func(w http.ResponseWriter, r *http.Request) {
		// Server-side restriction: only a session (bearer) may mint keys.
		if r.Header.Get("Authorization") != "Bearer "+AccessToken {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a session is required to create API keys")
			return
		}
		var body struct {
			Name      string `json:"name"`
			ExpiresIn int    `json:"expiresIn"`
		}
		if !decodeJSONBody(w, r, &body) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":        "key_mock0001",
			"name":      body.Name,
			"start":     "posi_mock",
			"prefix":    "posi_",
			"key":       APIKey,
			"expiresAt": "2026-09-08T09:00:00.000Z",
		})
	})

	return mux
}

// InstallHandler returns Handler plus the install contract: GET
// /api/souls/{slug}.md answers the soul's markdown body verbatim — the one
// endpoint that bumps the server's download counter. Read-command tests must
// keep using Handler (which answers 418 on .md so an accidental hit fails
// loudly); only install-path tests opt into this handler.
func InstallHandler() http.Handler {
	inner := Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug, isMD := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/api/souls/"), ".md")
		if r.Method == http.MethodGet && isMD &&
			strings.HasPrefix(r.URL.Path, "/api/souls/") && !strings.Contains(slug, "/") {
			for _, s := range Souls {
				if s.Slug == slug {
					w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
					_, _ = w.Write([]byte(s.Content))
					return
				}
			}
			writeError(w, http.StatusNotFound, "not_found", "Soul not found")
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// serveResearch implements GET /api/research over ResearchPosts: it filters by
// kind/category/tag/q, computes `latest` (the newest publishedAt within that
// filter, before `since`), then applies `since` and `limit` to the newest-first
// results — the same semantics the server documents, so golden output pins them.
func serveResearch(w http.ResponseWriter, sp url.Values) {
	kind, category, tag := sp.Get("kind"), sp.Get("category"), sp.Get("tag")
	q := strings.ToLower(sp.Get("q"))

	filtered := make([]api.ResearchItem, 0, len(ResearchPosts))
	for _, it := range ResearchPosts {
		if kind != "" && !strings.EqualFold(it.Kind, kind) {
			continue
		}
		if category != "" && !strings.EqualFold(it.Category, category) {
			continue
		}
		if tag != "" && !containsFoldStr(it.Tags, tag) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Title), q) &&
			!strings.Contains(strings.ToLower(it.Excerpt), q) {
			continue
		}
		filtered = append(filtered, it)
	}

	var latest *string
	for _, it := range filtered {
		if it.PublishedAt != nil && (latest == nil || *it.PublishedAt > *latest) {
			latest = it.PublishedAt
		}
	}

	since := sp.Get("since")
	results := make([]api.ResearchItem, 0, len(filtered))
	for _, it := range filtered {
		if since != "" && (it.PublishedAt == nil || *it.PublishedAt <= since) {
			continue
		}
		results = append(results, it)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return derefStr(results[i].PublishedAt) > derefStr(results[j].PublishedAt)
	})
	if n, err := strconv.Atoi(sp.Get("limit")); err == nil && n >= 0 && n < len(results) {
		results = results[:n]
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results, "latest": latest})
}

// containsFoldStr reports whether vals contains want, case-insensitively.
func containsFoldStr(vals []string, want string) bool {
	for _, v := range vals {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

// derefStr maps a nil *string to "" for ordering.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// authorized reports whether the request carries the fixture session token or
// the fixture API key.
func authorized(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+AccessToken ||
		r.Header.Get("x-api-key") == APIKey
}

// decodeJSONBody enforces the verified live behavior that the auth endpoints
// take JSON bodies only: anything else (e.g. RFC 8628 form encoding) is a
// 415, so a client regression is caught by every test using this mock.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"expected application/json")
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		writeOAuthError(w, "invalid_request", "malformed JSON body")
		return false
	}
	return true
}

// writeOAuthError writes the flat OAuth error shape the device endpoints use
// — distinct from the API's {"error":{...}} envelope.
func writeOAuthError(w http.ResponseWriter, code, description string) {
	body := map[string]string{"error": code}
	if description != "" {
		body["error_description"] = description
	}
	writeJSON(w, http.StatusBadRequest, body)
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
