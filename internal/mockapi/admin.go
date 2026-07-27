package mockapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/positronick/cli/internal/api"
)

// Admin write API (/api/admin/*) over the same frozen fixtures. Mutations are
// in-memory and PER Handler() — each test server starts from the pristine
// dataset (seeded rows carry source "seed", so the first patch flips
// ownership exactly like production). Validation mirrors the product repo's
// seed validators: messages are part of the contract because the CLI promises
// to surface them verbatim.

// Admin fixture credentials. AdminAPIKey/AdminToken authenticate as an admin;
// PlebToken authenticates as a regular user (403 on admin routes); anything
// else is 401.
const (
	AdminAPIKey = "posi_admin_test"
	AdminToken  = "admin-token"
	PlebToken   = "pleb-token"
)

// Fixture timestamps for rows created/updated through the admin API, so
// golden files stay byte-stable.
const (
	createdStamp = "2026-06-09T09:00:00.000Z"
	updatedStamp = "2026-06-09T10:00:00.000Z"
	// seedStamp dates the synthetic curated-author profiles (the mock has no
	// separate profile fixture; they are derived from the listing authors).
	seedStamp = "2026-06-08T00:00:00.000Z"
)

// Enum fixtures mirroring src/lib/types.ts in the product repo.
var (
	soulCategories    = []string{"Technical", "Professional", "Creative", "Educational", "Wellness", "Research", "Experimental", "Playful"}
	soulFrameworks    = []string{"hermes", "openclaw", "claude-code", "cursor"}
	listingCategories = []string{"AI/ML", "DevOps", "Cloud", "Web", "Data", "Security", "Technical", "Productivity"}
	statuses          = []string{"draft", "pending", "published"}
	// blogCategories mirrors BLOG_CATEGORIES — feed defaultCategory validation.
	blogCategories = []string{"Releases", "Announcements", "Tutorials", "Guides", "Engineering", "Community"}
	feedKinds      = []string{"github_release", "rss"}
)

// Patchable field sets, mirroring SOUL_PATCH_FIELDS / LISTING_PATCH_FIELDS in
// the product repo; create accepts the same minus "source".
var (
	soulPatchFields = []string{"slug", "slugHistory", "name", "authorHandle", "authorName",
		"authorUrl", "tagline", "description", "category", "tags", "frameworks", "models",
		"version", "license", "repoUrl", "content", "status", "source"}
	listingPatchFields = []string{"slug", "name", "type", "tagline", "description", "category",
		"tags", "sourceUrl", "repoUrl", "installCmd", "data", "confidence", "status", "source",
		"profileHandle"}
	// profileCreateFields mirrors PROFILE_CREATE_FIELDS in the product repo.
	profileCreateFields = []string{"handle", "name", "kind", "avatarUrl", "website",
		"githubUrl", "githubUserId", "bio", "socials", "verified", "official"}
	// feedCreateFields mirrors FEED_FIELDS in the product repo (feedFields.ts).
	feedCreateFields = []string{"label", "feedUrl", "kind", "authorHandle", "listingSlug",
		"defaultCategory", "defaultTags", "autoPublish", "enabled"}
)

// adminState is one Handler instance's mutable admin dataset.
type adminState struct {
	mu         sync.Mutex
	souls      []soulRow
	listings   []listingRow
	profiles   []profileRow
	feeds      []feedRow
	soulSeq    int
	listingSeq int
	profileSeq int
	feedSeq    int
}

// feedRow is the mock's in-memory feed source. Display handles/slugs are
// stored alongside the resolved ids so list/get responses match production's
// joined shape without a second lookup.
type feedRow struct {
	api.AdminFeed
}

type soulRow struct {
	api.Soul
	Source string
}

type listingRow struct {
	api.Listing
	Source string
}

type profileRow struct {
	api.Profile
	Source string
}

// registerAdmin mounts the admin write API on mux with a fresh copy of the
// fixtures.
func registerAdmin(mux *http.ServeMux) {
	st := &adminState{}
	for _, s := range Souls {
		st.souls = append(st.souls, soulRow{Soul: s, Source: "seed"})
	}
	for _, l := range Listings {
		st.listings = append(st.listings, listingRow{Listing: l, Source: "seed"})
	}
	st.profiles = seedProfiles()

	mux.HandleFunc("POST /api/admin/souls", st.createSoul)
	mux.HandleFunc("GET /api/admin/souls/{id}", st.getSoul)
	mux.HandleFunc("PATCH /api/admin/souls/{id}", st.patchSoul)
	mux.HandleFunc("POST /api/admin/listings", st.createListing)
	mux.HandleFunc("GET /api/admin/listings/{id}", st.getListing)
	mux.HandleFunc("PATCH /api/admin/listings/{id}", st.patchListing)
	mux.HandleFunc("POST /api/admin/profiles", st.createProfile)
	mux.HandleFunc("GET /api/admin/profiles", st.listProfiles)
	mux.HandleFunc("GET /api/admin/feeds", st.listFeeds)
	mux.HandleFunc("POST /api/admin/feeds", st.createFeed)
	mux.HandleFunc("GET /api/admin/feeds/{id}", st.getFeed)
	mux.HandleFunc("PATCH /api/admin/feeds/{id}", st.patchFeed)
	mux.HandleFunc("POST /api/admin/feeds/{id}/sync", st.syncFeed)
}

// seedProfiles builds the fixture's curated authors from the listing handles
// with stable synthetic metadata — the mock has no separate profile fixture,
// and the admin profiles list must show the existing authors a listing can
// reference. Sorted so ids and order stay deterministic for golden output.
func seedProfiles() []profileRow {
	names := knownProfiles()
	handles := make([]string, 0, len(names))
	for h := range names {
		handles = append(handles, h)
	}
	sort.Strings(handles)
	rows := make([]profileRow, len(handles))
	for i, h := range handles {
		rows[i] = profileRow{
			Profile: api.Profile{
				ID:        fmt.Sprintf("01PROFILE%017d", i+1),
				Handle:    h,
				Name:      names[h],
				Kind:      "org",
				Verified:  true,
				AvatarURL: ptr("https://github.com/" + h + ".png"),
				Socials:   []string{},
				CreatedAt: seedStamp,
				UpdatedAt: seedStamp,
			},
			Source: "seed",
		}
	}
	return rows
}

// adminDenied applies the 401/403 ladder and reports whether the request was
// rejected.
func adminDenied(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.Header.Get("x-api-key") == AdminAPIKey,
		r.Header.Get("Authorization") == "Bearer "+AdminToken:
		return false
	case r.Header.Get("Authorization") == "Bearer "+PlebToken:
		writeError(w, http.StatusForbidden, "forbidden", "admin access required")
		return true
	default:
		writeError(w, http.StatusUnauthorized, "unauthorized",
			"authentication required — run `positronick login`")
		return true
	}
}

// readBody decodes a JSON object body, answering 400 like the server does.
func readBody(w http.ResponseWriter, r *http.Request) map[string]any {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "request body must be a JSON object")
		return nil
	}
	return body
}

// invalid writes the 422 invalid_input envelope with msg verbatim.
func invalid(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusUnprocessableEntity, "invalid_input", msg)
}

func unknownKey(body map[string]any, allowed []string) (string, bool) {
	for k := range body {
		if !slices.Contains(allowed, k) {
			return k, true
		}
	}
	return "", false
}

// without returns fields minus the given keys — create accepts the patch
// field set minus "source", exactly like the server's CREATE_FIELDS.
func without(fields []string, drop ...string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !slices.Contains(drop, f) {
			out = append(out, f)
		}
	}
	return out
}

// blank reports whether a JSON value is missing for a required field: absent
// handling is the caller's; nil and whitespace-only strings are blank.
func blank(v any) bool {
	return v == nil || strings.TrimSpace(toStr(v)) == ""
}

func toStr(v any) string { return fmt.Sprint(v) }

func toStrSlice(v any) []string {
	switch vv := v.(type) {
	case nil:
		return []string{}
	case []any:
		out := make([]string, len(vv))
		for i, e := range vv {
			out[i] = toStr(e)
		}
		return out
	default:
		return []string{toStr(v)}
	}
}

func toStrPtr(v any) *string {
	if v == nil {
		return nil
	}
	return ptr(toStr(v))
}

// normalizeBody mirrors the seed's normalizeContent: CRLF→LF, trimmed.
func normalizeBody(content string) string {
	return strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
}

// bodyHash mirrors the seed's contentHash: sha256 of the normalized body.
func bodyHash(content string) string {
	sum := sha256.Sum256([]byte(normalizeBody(content)))
	return hex.EncodeToString(sum[:])
}

// ── Souls ────────────────────────────────────────────────────────────────────

func (st *adminState) createSoul(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	body := readBody(w, r)
	if body == nil {
		return
	}
	if _, ok := body["id"]; ok {
		invalid(w, `ids are server-assigned — omit "id"`)
		return
	}
	const ctx = "admin soul create"
	if key, ok := unknownKey(body, without(soulPatchFields, "source")); ok {
		invalid(w, fmt.Sprintf("%s: unknown field %q", ctx, key))
		return
	}
	if msg, ok := validateSoulPatch(ctx, body); !ok {
		invalid(w, msg)
		return
	}
	for _, key := range []string{"slug", "name", "authorHandle", "tagline", "category", "version", "license"} {
		if blank(body[key]) {
			invalid(w, fmt.Sprintf("%s: missing required field %q", ctx, key))
			return
		}
	}
	if normalizeBody(toStr(body["content"])) == "" {
		invalid(w, ctx+": SOUL.md body is empty")
		return
	}
	if len(toStrSlice(body["frameworks"])) == 0 {
		invalid(w, ctx+": at least one framework is required")
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	slug := toStr(body["slug"])
	if st.soulBySlug(slug) != nil {
		writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("slug %q is already taken", slug))
		return
	}
	st.soulSeq++
	content := normalizeBody(toStr(body["content"]))
	status := "published"
	if v, ok := body["status"]; ok {
		status = toStr(v)
	}
	row := soulRow{
		Soul: api.Soul{
			SoulCard: api.SoulCard{
				ID:           fmt.Sprintf("01SCREATED%016d", st.soulSeq),
				Slug:         slug,
				SlugHistory:  toStrSlice(body["slugHistory"]),
				Name:         toStr(body["name"]),
				AuthorHandle: toStr(body["authorHandle"]),
				AuthorName:   toStrPtr(body["authorName"]),
				AuthorURL:    toStrPtr(body["authorUrl"]),
				Tagline:      toStr(body["tagline"]),
				Description:  toStrPtr(body["description"]),
				Category:     toStr(body["category"]),
				Tags:         toStrSlice(body["tags"]),
				Frameworks:   toStrSlice(body["frameworks"]),
				Models:       toStrSlice(body["models"]),
				Version:      toStr(body["version"]),
				License:      toStr(body["license"]),
				RepoURL:      toStrPtr(body["repoUrl"]),
				ContentHash:  bodyHash(content),
				Status:       status,
				CreatedAt:    createdStamp,
				UpdatedAt:    createdStamp,
			},
			Content: content,
		},
		Source: "api",
	}
	st.souls = append(st.souls, row)
	writeJSON(w, http.StatusCreated, map[string]any{"soul": adminSoulJSON(row)})
}

func (st *adminState) getSoul(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("id")
	for i := range st.souls {
		if st.souls[i].ID == id {
			writeJSON(w, http.StatusOK, map[string]any{"soul": adminSoulJSON(st.souls[i])})
			return
		}
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("soul id %q not found", id))
}

func (st *adminState) patchSoul(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	body := readBody(w, r)
	if body == nil {
		return
	}
	const ctx = "admin soul update"
	if key, ok := unknownKey(body, soulPatchFields); ok {
		invalid(w, fmt.Sprintf("%s: unknown field %q", ctx, key))
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("id")
	var row *soulRow
	for i := range st.souls {
		if st.souls[i].ID == id {
			row = &st.souls[i]
			break
		}
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("soul id %q not found", id))
		return
	}
	if msg, ok := validateSoulPatch(ctx, body); !ok {
		invalid(w, msg)
		return
	}
	source, took, ok := resolveSource(w, ctx, row.Source, body)
	if !ok {
		return
	}
	for _, key := range []string{"slug", "name", "authorHandle", "tagline", "category", "version", "license", "content"} {
		if v, present := body[key]; present && blank(v) {
			invalid(w, fmt.Sprintf("%s: missing required field %q", ctx, key))
			return
		}
	}

	if v, ok := body["slug"]; ok {
		slug := toStr(v)
		if slug != row.Slug {
			if st.soulBySlug(slug) != nil {
				writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("slug %q is already taken", slug))
				return
			}
			if _, explicit := body["slugHistory"]; !explicit {
				row.SlugHistory = append(slices.Clone(row.SlugHistory), row.Slug)
			}
			row.Slug = slug
		}
	}
	if v, ok := body["slugHistory"]; ok {
		row.SlugHistory = toStrSlice(v)
	}
	setStr := func(key string, dst *string) {
		if v, ok := body[key]; ok {
			*dst = toStr(v)
		}
	}
	setPtr := func(key string, dst **string) {
		if v, ok := body[key]; ok {
			*dst = toStrPtr(v)
		}
	}
	setSlice := func(key string, dst *[]string) {
		if v, ok := body[key]; ok {
			*dst = toStrSlice(v)
		}
	}
	setStr("name", &row.Name)
	setStr("authorHandle", &row.AuthorHandle)
	setPtr("authorName", &row.AuthorName)
	setPtr("authorUrl", &row.AuthorURL)
	setStr("tagline", &row.Tagline)
	setPtr("description", &row.Description)
	setStr("category", &row.Category)
	setSlice("tags", &row.Tags)
	setSlice("frameworks", &row.Frameworks)
	setSlice("models", &row.Models)
	setStr("version", &row.Version)
	setStr("license", &row.License)
	setPtr("repoUrl", &row.RepoURL)
	setStr("status", &row.Status)
	if v, ok := body["content"]; ok {
		row.Content = normalizeBody(toStr(v))
		row.ContentHash = bodyHash(row.Content)
	}
	row.Source = source
	row.UpdatedAt = updatedStamp
	writeJSON(w, http.StatusOK, map[string]any{"soul": adminSoulJSON(*row), "tookOwnership": took})
}

// validateSoulPatch checks the enum-valued soul fields present in body,
// returning the validator message on the first violation.
func validateSoulPatch(ctx string, body map[string]any) (string, bool) {
	if v, ok := body["status"]; ok && !slices.Contains(statuses, toStr(v)) {
		return fmt.Sprintf("%s: invalid status %q (expected one of: %s)",
			ctx, toStr(v), strings.Join(statuses, ", ")), false
	}
	if v, ok := body["category"]; ok && !slices.Contains(soulCategories, toStr(v)) {
		return fmt.Sprintf("%s: invalid category %q (expected one of: %s)",
			ctx, toStr(v), strings.Join(soulCategories, ", ")), false
	}
	if v, ok := body["frameworks"]; ok {
		for _, f := range toStrSlice(v) {
			if !slices.Contains(soulFrameworks, f) {
				return fmt.Sprintf("%s: invalid framework %q (expected one of: %s)",
					ctx, f, strings.Join(soulFrameworks, ", ")), false
			}
		}
	}
	return "", true
}

// resolveSource applies the ownership rules: an explicit source wins (and
// must be "seed" or "api"); otherwise any change takes "api" ownership.
func resolveSource(w http.ResponseWriter, ctx, existing string, body map[string]any) (source string, tookOwnership, ok bool) {
	source = "api"
	if v, present := body["source"]; present {
		source = toStr(v)
		if source != "seed" && source != "api" {
			invalid(w, fmt.Sprintf(`%s: invalid source %q (expected "seed" or "api")`, ctx, source))
			return "", false, false
		}
	}
	return source, existing == "seed" && source == "api", true
}

func (st *adminState) soulBySlug(slug string) *soulRow {
	for i := range st.souls {
		if st.souls[i].Slug == slug {
			return &st.souls[i]
		}
	}
	return nil
}

// adminSoulJSON renders a row the way the admin API does: the public soul
// shape plus source.
func adminSoulJSON(row soulRow) map[string]any {
	return withSource(row.Soul, row.Source)
}

func adminListingJSON(row listingRow) map[string]any {
	return withSource(row.Listing, row.Source)
}

// withSource marshals v and grafts on the source field — the admin responses
// are the public wire shape plus "source".
func withSource(v any, source string) map[string]any {
	raw, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["source"] = source
	return m
}

// ── Listings ─────────────────────────────────────────────────────────────────

func (st *adminState) createListing(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	body := readBody(w, r)
	if body == nil {
		return
	}
	if _, ok := body["id"]; ok {
		invalid(w, `ids are server-assigned — omit "id"`)
		return
	}
	const ctx = "admin listing create"
	if key, ok := unknownKey(body, without(listingPatchFields, "source")); ok {
		invalid(w, fmt.Sprintf("%s: unknown field %q", ctx, key))
		return
	}
	if v, ok := body["status"]; ok && !slices.Contains(statuses, toStr(v)) {
		invalid(w, fmt.Sprintf("%s: invalid status %q (expected one of: %s)",
			ctx, toStr(v), strings.Join(statuses, ", ")))
		return
	}
	if blank(body["profileHandle"]) {
		invalid(w, ctx+`: missing required field "profileHandle"`)
		return
	}
	for _, key := range []string{"slug", "name", "type", "tagline", "category", "sourceUrl"} {
		if blank(body[key]) {
			invalid(w, fmt.Sprintf("%s: missing required field %q", ctx, key))
			return
		}
	}
	if msg, ok := validateListingPatch(ctx, toStr(body["slug"]), body, body["data"]); !ok {
		invalid(w, msg)
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	profile := st.profileByHandle(toStr(body["profileHandle"]))
	if profile == nil {
		writeError(w, http.StatusUnprocessableEntity, "unknown_profile",
			fmt.Sprintf("profile %q does not exist", toStr(body["profileHandle"])))
		return
	}
	slug := toStr(body["slug"])
	if st.listingBySlug(slug) != nil {
		writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("slug %q is already taken", slug))
		return
	}
	st.listingSeq++
	data, _ := body["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	status := "published"
	if v, ok := body["status"]; ok {
		status = toStr(v)
	}
	confidence := "high"
	if v, ok := body["confidence"]; ok {
		confidence = toStr(v)
	}
	row := listingRow{
		Listing: api.Listing{
			ID:            fmt.Sprintf("01XCREATED%016d", st.listingSeq),
			Slug:          slug,
			ProfileHandle: toStr(body["profileHandle"]),
			ProfileName:   profile.Name,
			Name:          toStr(body["name"]),
			Type:          toStr(body["type"]),
			Tagline:       toStr(body["tagline"]),
			Description:   toStrPtr(body["description"]),
			Category:      toStr(body["category"]),
			Tags:          toStrSlice(body["tags"]),
			Official:      true,
			SourceURL:     toStr(body["sourceUrl"]),
			RepoURL:       toStrPtr(body["repoUrl"]),
			InstallCmd:    toStrPtr(body["installCmd"]),
			Data:          data,
			Confidence:    confidence,
			Status:        status,
			DownloadCount: 0,
			CreatedAt:     createdStamp,
			UpdatedAt:     createdStamp,
		},
		Source: "api",
	}
	st.listings = append(st.listings, row)
	writeJSON(w, http.StatusCreated, map[string]any{"listing": adminListingJSON(row)})
}

func (st *adminState) getListing(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("id")
	for i := range st.listings {
		if st.listings[i].ID == id {
			writeJSON(w, http.StatusOK, map[string]any{"listing": adminListingJSON(st.listings[i])})
			return
		}
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("listing id %q not found", id))
}

func (st *adminState) patchListing(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	body := readBody(w, r)
	if body == nil {
		return
	}
	const ctx = "admin listing update"
	if key, ok := unknownKey(body, listingPatchFields); ok {
		invalid(w, fmt.Sprintf("%s: unknown field %q", ctx, key))
		return
	}
	if v, ok := body["status"]; ok && !slices.Contains(statuses, toStr(v)) {
		invalid(w, fmt.Sprintf("%s: invalid status %q (expected one of: %s)",
			ctx, toStr(v), strings.Join(statuses, ", ")))
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("id")
	var row *listingRow
	for i := range st.listings {
		if st.listings[i].ID == id {
			row = &st.listings[i]
			break
		}
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("listing id %q not found", id))
		return
	}
	source, took, ok := resolveSource(w, ctx, row.Source, body)
	if !ok {
		return
	}
	if v, present := body["profileHandle"]; present {
		profile := st.profileByHandle(toStr(v))
		if profile == nil {
			writeError(w, http.StatusUnprocessableEntity, "unknown_profile",
				fmt.Sprintf("profile %q does not exist", toStr(v)))
			return
		}
		row.ProfileHandle, row.ProfileName = toStr(v), profile.Name
	}
	if v, ok := body["slug"]; ok {
		slug := toStr(v)
		if slug != row.Slug && st.listingBySlug(slug) != nil {
			writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("slug %q is already taken", slug))
			return
		}
		row.Slug = slug
	}
	merged := row.Data
	if v, ok := body["data"]; ok {
		if m, isMap := v.(map[string]any); isMap {
			merged = m
		} else {
			merged = map[string]any{}
		}
	}
	finalType := row.Type
	if v, ok := body["type"]; ok {
		finalType = toStr(v)
	}
	if msg, ok := validateListingPatch(ctx, row.Slug, map[string]any{"type": finalType}, merged); !ok {
		invalid(w, msg)
		return
	}
	if v, ok := body["type"]; ok {
		row.Type = toStr(v)
	}
	if v, ok := body["name"]; ok {
		row.Name = toStr(v)
	}
	if v, ok := body["tagline"]; ok {
		row.Tagline = toStr(v)
	}
	if v, ok := body["description"]; ok {
		row.Description = toStrPtr(v)
	}
	if v, ok := body["category"]; ok {
		row.Category = toStr(v)
	}
	if v, ok := body["tags"]; ok {
		row.Tags = toStrSlice(v)
	}
	if v, ok := body["sourceUrl"]; ok {
		row.SourceURL = toStr(v)
	}
	if v, ok := body["repoUrl"]; ok {
		row.RepoURL = toStrPtr(v)
	}
	if v, ok := body["installCmd"]; ok {
		row.InstallCmd = toStrPtr(v)
	}
	if v, ok := body["confidence"]; ok {
		row.Confidence = toStr(v)
	}
	if v, ok := body["status"]; ok {
		row.Status = toStr(v)
	}
	row.Data = merged
	row.Source = source
	row.UpdatedAt = updatedStamp
	writeJSON(w, http.StatusOK, map[string]any{"listing": adminListingJSON(*row), "tookOwnership": took})
}

// validateListingPatch checks type/category enums and the loop data
// requirements over the (merged) values.
func validateListingPatch(ctx, slug string, body map[string]any, data any) (string, bool) {
	listingType := ""
	if v, ok := body["type"]; ok {
		listingType = toStr(v)
		if !slices.Contains(api.ListingTypes, listingType) {
			return fmt.Sprintf("%s: invalid type %q (expected one of: %s)",
				ctx, listingType, strings.Join(api.ListingTypes, ", ")), false
		}
	}
	if v, ok := body["category"]; ok && !slices.Contains(listingCategories, toStr(v)) {
		return fmt.Sprintf("%s: invalid category %q (expected one of: %s)",
			ctx, toStr(v), strings.Join(listingCategories, ", ")), false
	}
	if listingType == "loop" {
		m, _ := data.(map[string]any)
		for _, key := range []string{"goal", "checkCommand", "exitCondition"} {
			if m == nil || blank(m[key]) {
				return fmt.Sprintf("%s (%s): loop is missing required data.%s", ctx, slug, key), false
			}
		}
	}
	return "", true
}

func (st *adminState) listingBySlug(slug string) *listingRow {
	for i := range st.listings {
		if st.listings[i].Slug == slug {
			return &st.listings[i]
		}
	}
	return nil
}

// knownProfiles maps the fixture profile handles to display names — profiles
// stay git-curated, so the admin API only references existing ones.
func knownProfiles() map[string]string {
	m := make(map[string]string, len(Listings))
	for _, l := range Listings {
		m[l.ProfileHandle] = l.ProfileName
	}
	return m
}

// ── Profiles ─────────────────────────────────────────────────────────────────

func (st *adminState) createProfile(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	body := readBody(w, r)
	if body == nil {
		return
	}
	const ctx = "admin profile create"
	if _, ok := body["id"]; ok {
		invalid(w, ctx+`: ids are server-assigned — omit "id"`)
		return
	}
	if key, ok := unknownKey(body, profileCreateFields); ok {
		invalid(w, fmt.Sprintf("%s: unknown field %q", ctx, key))
		return
	}
	for _, key := range []string{"handle", "name", "kind"} {
		if blank(body[key]) {
			invalid(w, fmt.Sprintf("%s: missing required field %q", ctx, key))
			return
		}
	}
	kind := toStr(body["kind"])
	if kind != "person" && kind != "org" {
		invalid(w, fmt.Sprintf("%s: invalid kind %q (expected person | org)", ctx, kind))
		return
	}
	handle := toStr(body["handle"])

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.profileByHandle(handle) != nil {
		writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("handle %q is already taken", handle))
		return
	}
	st.profileSeq++
	avatar := toStrPtr(body["avatarUrl"])
	if avatar == nil {
		avatar = ptr("https://github.com/" + handle + ".png")
	}
	row := profileRow{
		Profile: api.Profile{
			ID:           fmt.Sprintf("01PCREATED%016d", st.profileSeq),
			Handle:       handle,
			Name:         toStr(body["name"]),
			Kind:         kind,
			Verified:     body["verified"] == true,
			Official:     body["official"] == true,
			Website:      toStrPtr(body["website"]),
			GithubURL:    toStrPtr(body["githubUrl"]),
			GithubUserID: toStrPtr(body["githubUserId"]),
			AvatarURL:    avatar,
			Bio:          toStrPtr(body["bio"]),
			Socials:      toStrSlice(body["socials"]),
			CreatedAt:    createdStamp,
			UpdatedAt:    createdStamp,
		},
		Source: "api",
	}
	st.profiles = append(st.profiles, row)
	writeJSON(w, http.StatusCreated, map[string]any{"profile": withSource(row.Profile, row.Source)})
}

func (st *adminState) listProfiles(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]map[string]any, len(st.profiles))
	for i := range st.profiles {
		out[i] = withSource(st.profiles[i].Profile, st.profiles[i].Source)
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

func (st *adminState) profileByHandle(handle string) *profileRow {
	for i := range st.profiles {
		if st.profiles[i].Handle == handle {
			return &st.profiles[i]
		}
	}
	return nil
}

// ── Feeds ────────────────────────────────────────────────────────────────────

// listFeeds returns every feed source. The mock starts empty (no seed) so
// goldens pin an empty list; create populates the in-memory set.
func (st *adminState) listFeeds(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]api.AdminFeed, len(st.feeds))
	for i := range st.feeds {
		out[i] = st.feeds[i].AdminFeed
	}
	writeJSON(w, http.StatusOK, map[string]any{"feeds": out})
}

func (st *adminState) createFeed(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	body := readBody(w, r)
	if body == nil {
		return
	}
	const ctx = "admin feed create"
	if _, ok := body["id"]; ok {
		invalid(w, ctx+`: ids are server-assigned — omit "id"`)
		return
	}
	if key, ok := unknownKey(body, feedCreateFields); ok {
		invalid(w, fmt.Sprintf("%s: unknown field %q", ctx, key))
		return
	}
	for _, key := range []string{"label", "feedUrl", "kind", "defaultCategory"} {
		if blank(body[key]) {
			invalid(w, fmt.Sprintf("%s: missing required field %q", ctx, key))
			return
		}
	}
	kind := toStr(body["kind"])
	if !slices.Contains(feedKinds, kind) {
		invalid(w, fmt.Sprintf("%s: invalid kind %q (expected one of: %s)",
			ctx, kind, strings.Join(feedKinds, ", ")))
		return
	}
	category := toStr(body["defaultCategory"])
	if !slices.Contains(blogCategories, category) {
		invalid(w, fmt.Sprintf("%s: invalid defaultCategory %q (expected one of: %s)",
			ctx, category, strings.Join(blogCategories, ", ")))
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	var authorProfileID, authorHandle *string
	if v, ok := body["authorHandle"]; ok && !blank(v) {
		handle := toStr(v)
		profile := st.profileByHandle(handle)
		if profile == nil {
			writeError(w, http.StatusUnprocessableEntity, "unknown_profile",
				fmt.Sprintf("profile %q does not exist", handle))
			return
		}
		authorProfileID = ptr(profile.ID)
		authorHandle = ptr(handle)
	}

	var listingID, listingSlug *string
	if v, ok := body["listingSlug"]; ok && !blank(v) {
		slug := toStr(v)
		listing := st.listingBySlug(slug)
		if listing == nil {
			// invalid_input (not unknown_listing): product feeds.ts uses invalid_input for unknown listing.
			writeError(w, http.StatusUnprocessableEntity, "invalid_input",
				fmt.Sprintf("listing %q does not exist", slug))
			return
		}
		listingID = ptr(listing.ID)
		listingSlug = ptr(slug)
	}

	tags := []string{}
	if v, ok := body["defaultTags"]; ok {
		tags = toStrSlice(v)
	}
	autoPublish := toBool(body["autoPublish"], false)
	enabled := toBool(body["enabled"], true)

	st.feedSeq++
	row := feedRow{
		AdminFeed: api.AdminFeed{
			ID:              fmt.Sprintf("01FCREATED%016d", st.feedSeq),
			Label:           toStr(body["label"]),
			FeedURL:         toStr(body["feedUrl"]),
			Kind:            kind,
			AuthorProfileID: authorProfileID,
			AuthorHandle:    authorHandle,
			ListingID:       listingID,
			ListingSlug:     listingSlug,
			DefaultCategory: category,
			DefaultTags:     tags,
			AutoPublish:     autoPublish,
			Enabled:         enabled,
			CreatedAt:       createdStamp,
			UpdatedAt:       createdStamp,
		},
	}
	st.feeds = append(st.feeds, row)
	writeJSON(w, http.StatusCreated, map[string]any{"feed": row.AdminFeed})
}

func (st *adminState) getFeed(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("id")
	if row := st.feedByID(id); row != nil {
		writeJSON(w, http.StatusOK, map[string]any{"feed": row.AdminFeed})
		return
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("feed source %q not found", id))
}

// patchFeed validates every field (and resolves author/listing) into locals
// first, then applies all row mutations once. Partial writes must not survive
// a late 422 on author/listing — product feeds.ts is all-or-nothing too.
func (st *adminState) patchFeed(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	body := readBody(w, r)
	if body == nil {
		return
	}
	const ctx = "admin feed update"
	if key, ok := unknownKey(body, feedCreateFields); ok {
		invalid(w, fmt.Sprintf("%s: unknown field %q", ctx, key))
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("id")
	row := st.feedByID(id)
	if row == nil {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("feed source %q not found", id))
		return
	}

	// ── Phase 1: validate + resolve into locals (no row writes) ───────────
	var (
		kind, category, label, feedURL *string
		tags                           *[]string
		autoPublish, enabled           *bool
		setAuthor, setListing          bool
		authorProfileID, authorHandle  *string
		listingID, listingSlug         *string
	)

	if v, ok := body["kind"]; ok {
		k := toStr(v)
		if !slices.Contains(feedKinds, k) {
			invalid(w, fmt.Sprintf("%s: invalid kind %q (expected one of: %s)",
				ctx, k, strings.Join(feedKinds, ", ")))
			return
		}
		kind = &k
	}
	if v, ok := body["defaultCategory"]; ok {
		c := toStr(v)
		if !slices.Contains(blogCategories, c) {
			invalid(w, fmt.Sprintf("%s: invalid defaultCategory %q (expected one of: %s)",
				ctx, c, strings.Join(blogCategories, ", ")))
			return
		}
		category = &c
	}
	if v, ok := body["label"]; ok {
		if blank(v) {
			invalid(w, fmt.Sprintf("%s: missing required field %q", ctx, "label"))
			return
		}
		s := toStr(v)
		label = &s
	}
	if v, ok := body["feedUrl"]; ok {
		if blank(v) {
			invalid(w, fmt.Sprintf("%s: missing required field %q", ctx, "feedUrl"))
			return
		}
		s := toStr(v)
		feedURL = &s
	}
	if v, ok := body["defaultTags"]; ok {
		t := toStrSlice(v)
		tags = &t
	}
	if v, ok := body["autoPublish"]; ok {
		b := toBool(v, row.AutoPublish)
		autoPublish = &b
	}
	if v, ok := body["enabled"]; ok {
		b := toBool(v, row.Enabled)
		enabled = &b
	}
	// unknown author → unknown_profile; unknown listing → invalid_input
	// (product feeds.ts intentional asymmetry — do not unify the codes).
	if v, present := body["authorHandle"]; present {
		setAuthor = true
		if !blank(v) {
			handle := toStr(v)
			profile := st.profileByHandle(handle)
			if profile == nil {
				writeError(w, http.StatusUnprocessableEntity, "unknown_profile",
					fmt.Sprintf("profile %q does not exist", handle))
				return
			}
			authorProfileID = ptr(profile.ID)
			authorHandle = ptr(handle)
		}
		// blank clears attribution (both nil)
	}
	if v, present := body["listingSlug"]; present {
		setListing = true
		if !blank(v) {
			slug := toStr(v)
			listing := st.listingBySlug(slug)
			if listing == nil {
				// invalid_input (not unknown_listing): product feeds.ts uses invalid_input for unknown listing.
				writeError(w, http.StatusUnprocessableEntity, "invalid_input",
					fmt.Sprintf("listing %q does not exist", slug))
				return
			}
			listingID = ptr(listing.ID)
			listingSlug = ptr(slug)
		}
		// blank clears attribution (both nil)
	}

	// ── Phase 2: apply all validated changes once ─────────────────────────
	if kind != nil {
		row.Kind = *kind
	}
	if category != nil {
		row.DefaultCategory = *category
	}
	if label != nil {
		row.Label = *label
	}
	if feedURL != nil {
		row.FeedURL = *feedURL
	}
	if tags != nil {
		row.DefaultTags = *tags
	}
	if autoPublish != nil {
		row.AutoPublish = *autoPublish
	}
	if enabled != nil {
		row.Enabled = *enabled
	}
	if setAuthor {
		row.AuthorProfileID, row.AuthorHandle = authorProfileID, authorHandle
	}
	if setListing {
		row.ListingID, row.ListingSlug = listingID, listingSlug
	}
	row.UpdatedAt = updatedStamp
	writeJSON(w, http.StatusOK, map[string]any{"feed": row.AdminFeed})
}

// syncFeed fakes a successful ingest with no network: stamps lastFetchedAt
// and returns a deterministic summary so CLI goldens stay stable offline.
func (st *adminState) syncFeed(w http.ResponseWriter, r *http.Request) {
	if adminDenied(w, r) {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	id := r.PathValue("id")
	row := st.feedByID(id)
	if row == nil {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("feed source %q not found", id))
		return
	}
	row.LastFetchedAt = ptr(updatedStamp)
	row.LastStatus = ptr("ok")
	row.UpdatedAt = updatedStamp
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": api.FeedSyncSummary{
			FeedID:     row.ID,
			Label:      row.Label,
			Fetched:    1,
			Created:    1,
			Updated:    0,
			Skipped:    0,
			ItemErrors: []string{},
		},
	})
}

func (st *adminState) feedByID(id string) *feedRow {
	for i := range st.feeds {
		if st.feeds[i].ID == id {
			return &st.feeds[i]
		}
	}
	return nil
}

// toBool coerces a JSON value to bool; nil/unknown fall back to defaultVal.
func toBool(v any, defaultVal bool) bool {
	if v == nil {
		return defaultVal
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	default:
		return defaultVal
	}
}
