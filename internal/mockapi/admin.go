package mockapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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
)

// Enum fixtures mirroring src/lib/types.ts in the product repo.
var (
	soulCategories    = []string{"Technical", "Professional", "Creative", "Educational", "Wellness", "Research", "Experimental", "Playful"}
	soulFrameworks    = []string{"hermes", "openclaw", "claude-code", "cursor"}
	listingCategories = []string{"AI/ML", "DevOps", "Cloud", "Web", "Data", "Security", "Technical", "Productivity"}
	statuses          = []string{"draft", "pending", "published"}
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
)

// adminState is one Handler instance's mutable admin dataset.
type adminState struct {
	mu         sync.Mutex
	souls      []soulRow
	listings   []listingRow
	soulSeq    int
	listingSeq int
}

type soulRow struct {
	api.Soul
	Source string
}

type listingRow struct {
	api.Listing
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

	mux.HandleFunc("POST /api/admin/souls", st.createSoul)
	mux.HandleFunc("GET /api/admin/souls/{id}", st.getSoul)
	mux.HandleFunc("PATCH /api/admin/souls/{id}", st.patchSoul)
	mux.HandleFunc("POST /api/admin/listings", st.createListing)
	mux.HandleFunc("GET /api/admin/listings/{id}", st.getListing)
	mux.HandleFunc("PATCH /api/admin/listings/{id}", st.patchListing)
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
	profileName, ok := knownProfiles()[toStr(body["profileHandle"])]
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "unknown_profile",
			fmt.Sprintf("profile %q does not exist", toStr(body["profileHandle"])))
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
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
			ProfileName:   profileName,
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
		name, known := knownProfiles()[toStr(v)]
		if !known {
			writeError(w, http.StatusUnprocessableEntity, "unknown_profile",
				fmt.Sprintf("profile %q does not exist", toStr(v)))
			return
		}
		row.ProfileHandle, row.ProfileName = toStr(v), name
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
