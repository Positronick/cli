// Package api is the typed HTTP client for the positronick.com read API. It
// owns the wire types, the error envelope, auth-header injection, and the
// retry policy; callers receive decoded structs or a typed *APIError.
package api

import (
	"encoding/json"
	"fmt"
)

// This file mirrors src/lib/types.ts in the product repo field for field —
// that file is the single source of truth for the wire contract. Keep the two
// in lockstep: a field added there is added here with the same camelCase JSON
// name.
//
// Dates stay as ISO-8601 strings on purpose: the CLI never computes on them,
// and passing them through verbatim keeps --json output (a golden-file
// contract) byte-stable regardless of Go's time formatting.

// ListingTypes are the kinds of official tooling the registry catalogs.
// Mirrors LISTING_TYPES in src/lib/types.ts.
var ListingTypes = []string{"harness", "cli", "mcp", "agent", "skill", "plugin", "loop"}

// FeedKinds are the kinds of blog feed source the ingestor mirrors. Mirrors
// FEED_KINDS in src/lib/server/feedFields.ts.
var FeedKinds = []string{"github_release", "rss"}

// BlogCategories are the categories a mirrored post (and a feed source's
// defaultCategory) may use. Mirrors BLOG_CATEGORIES in src/lib/types.ts.
var BlogCategories = []string{"Releases", "Announcements", "Tutorials", "Guides", "Engineering", "Community"}

// SoulCard is the lightweight list/gallery view of a soul — everything needed
// to render a card or detail header, deliberately excluding the heavy
// markdown body. Mirrors SoulCard (= SoulMeta) in src/lib/types.ts.
//
// Nullable fields in the TS contract (string | null, number | null) are
// pointers so that null round-trips as null in --json output.
type SoulCard struct {
	// ID is the stable, immutable id (ULID). Never changes, even if the slug does.
	ID string `json:"id"`
	// Slug is the human-facing url segment: /souls/[slug].
	Slug string `json:"slug"`
	// SlugHistory holds previous slugs; the server 301s them to the current slug.
	SlugHistory []string `json:"slugHistory"`
	Name        string   `json:"name"`
	// AuthorHandle is the author handle — the join key for user accounts.
	AuthorHandle string  `json:"authorHandle"`
	AuthorName   *string `json:"authorName"`
	AuthorURL    *string `json:"authorUrl"`
	// Tagline is the short one-liner shown on cards.
	Tagline     string   `json:"tagline"`
	Description *string  `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Frameworks  []string `json:"frameworks"`
	Models      []string `json:"models"`
	// Version is semver.
	Version string `json:"version"`
	// License is an SPDX license id, e.g. "MIT".
	License string  `json:"license"`
	RepoURL *string `json:"repoUrl"`
	// ContentHash is the sha256 of the normalized SOUL.md body.
	ContentHash string `json:"contentHash"`
	// Status is draft | pending | published.
	Status        string   `json:"status"`
	DownloadCount int      `json:"downloadCount"`
	RatingAvg     *float64 `json:"ratingAvg"`
	RatingCount   int      `json:"ratingCount"`
	ArenaRank     *int     `json:"arenaRank"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

// Soul is a full soul, including the raw SOUL.md markdown body. Mirrors Soul
// in src/lib/types.ts (SoulMeta + content).
type Soul struct {
	SoulCard
	Content string `json:"content"`
}

// Listing is a catalog entry for one official tool. Mirrors Listing in
// src/lib/types.ts.
type Listing struct {
	// ID is the stable, immutable id (ULID).
	ID string `json:"id"`
	// Slug is the human-facing url segment: /listings/[slug].
	Slug string `json:"slug"`
	// ProfileHandle/ProfileName denormalize the authoring profile for cards.
	ProfileHandle string `json:"profileHandle"`
	ProfileName   string `json:"profileName"`
	Name          string `json:"name"`
	// Type is one of ListingTypes.
	Type string `json:"type"`
	// Tagline is the short one-liner shown on cards.
	Tagline     string   `json:"tagline"`
	Description *string  `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	// Official is true when published by the owner's verified official account.
	Official bool `json:"official"`
	// SourceURL is the official source that was verified: repo, docs, or registry.
	SourceURL string  `json:"sourceUrl"`
	RepoURL   *string `json:"repoUrl"`
	// InstallCmd is the canonical official install/run command, if any.
	InstallCmd *string `json:"installCmd"`
	// Data holds type-specific extras (e.g. LoopData for loops); {} when none.
	Data          map[string]any `json:"data"`
	Confidence    string         `json:"confidence"`
	Status        string         `json:"status"`
	DownloadCount int            `json:"downloadCount"`
	CreatedAt     string         `json:"createdAt"`
	UpdatedAt     string         `json:"updatedAt"`
}

// LoopData decodes the untyped Data payload into LoopData. Empty or nil Data
// yields the zero value; a wrong-typed field is an error, never a silent zero.
func (l *Listing) LoopData() (LoopData, error) {
	var ld LoopData
	if len(l.Data) == 0 {
		return ld, nil
	}
	b, err := json.Marshal(l.Data)
	if err != nil {
		return ld, fmt.Errorf("encoding listing data: %w", err)
	}
	if err := json.Unmarshal(b, &ld); err != nil {
		return ld, fmt.Errorf("decoding loop data for %q: %w", l.Slug, err)
	}
	return ld, nil
}

// Profile is a verified person or org that authors registry tooling. Mirrors
// Profile in src/lib/types.ts. Nullable TS fields (string | null) are pointers
// so null round-trips as null in --json output.
type Profile struct {
	// ID is the stable, immutable id (ULID).
	ID string `json:"id"`
	// Handle is the human-facing handle: /profiles/[handle].
	Handle string `json:"handle"`
	Name   string `json:"name"`
	// Kind is person | org.
	Kind string `json:"kind"`
	// Verified is the white seal (team-curated or claimed); Official is the red seal.
	Verified  bool    `json:"verified"`
	Official  bool    `json:"official"`
	Website   *string `json:"website"`
	GithubURL *string `json:"githubUrl"`
	// GithubUserID is the immutable GitHub numeric id (person profiles).
	GithubUserID *string `json:"githubUserId"`
	AvatarURL    *string `json:"avatarUrl"`
	Bio          *string `json:"bio"`
	// Socials are public http(s) URLs.
	Socials   []string `json:"socials"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// LoopData is the type-specific extras for a `loop` listing, stored in
// Listing.Data. Mirrors LoopData in src/lib/types.ts.
type LoopData struct {
	// Goal is what "done" looks like for the loop.
	Goal string `json:"goal,omitempty"`
	// CheckCommand is the command run between iterations to gauge progress.
	CheckCommand string `json:"checkCommand,omitempty"`
	// ExitCondition is the condition that ends the loop.
	ExitCondition string `json:"exitCondition,omitempty"`
	// MaxIterations is the safety cap on iterations.
	MaxIterations int `json:"maxIterations,omitempty"`
	// CompatibleTools lists agents/harnesses the loop is known to work with.
	CompatibleTools []string `json:"compatibleTools,omitempty"`
	// Kickoff is the prompt a user copies to start the loop.
	Kickoff string `json:"kickoff,omitempty"`
}

// FeedSource is a subscribed blog feed source the ingestor mirrors into posts
// (GitHub releases or RSS). Admin-only — never returned from a public route.
// Mirrors FeedSource in src/lib/server/feeds.ts. Nullable TS fields are
// pointers so null round-trips as null in --json output. There is deliberately
// no `source` field on the wire: feed sources are always api-owned (never
// git-seeded), so they carry no seed/api ownership marker.
type FeedSource struct {
	// ID is the stable, immutable id (ULID).
	ID    string `json:"id"`
	Label string `json:"label"`
	// FeedURL is the github_release repo URL or the rss feed URL.
	FeedURL string `json:"feedUrl"`
	// Kind is one of FeedKinds: github_release | rss.
	Kind string `json:"kind"`
	// AuthorProfileID/AuthorHandle are the default byline stamped on mirrored
	// posts; the handle is left-joined for display. Null when unattributed.
	AuthorProfileID *string `json:"authorProfileId"`
	AuthorHandle    *string `json:"authorHandle"`
	// ListingID/ListingSlug are the related tool stamped on mirrored posts; the
	// slug is left-joined for display. Null when none.
	ListingID   *string `json:"listingId"`
	ListingSlug *string `json:"listingSlug"`
	// DefaultCategory is one of BlogCategories.
	DefaultCategory string   `json:"defaultCategory"`
	DefaultTags     []string `json:"defaultTags"`
	// AutoPublish publishes mirrored posts immediately instead of as drafts.
	AutoPublish bool `json:"autoPublish"`
	// Enabled is the on/off switch; false pauses the feed (there is no delete).
	Enabled bool `json:"enabled"`
	// LastFetchedAt/LastStatus record the last ingest run — null until the
	// first run; LastStatus is "ok" or the last error message (fail-loud).
	LastFetchedAt *string `json:"lastFetchedAt"`
	LastStatus    *string `json:"lastStatus"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

// FeedSyncSummary is the per-feed ingest outcome from a sync. Mirrors
// IngestSummary in src/lib/server/feedIngest.ts. A non-empty Error means the
// fetch/parse failed and no items were processed — the sync endpoint answers
// 502 in that case, with this same summary as the body.
type FeedSyncSummary struct {
	FeedID  string `json:"feedId"`
	Label   string `json:"label"`
	Fetched int    `json:"fetched"`
	Created int    `json:"created"`
	Updated int    `json:"updated"`
	Skipped int    `json:"skipped"`
	// ItemErrors holds per-item failures on an otherwise-successful run; each
	// such item is counted in Skipped.
	ItemErrors []string `json:"itemErrors"`
	// Error is the fetch/parse failure reason; empty on success.
	Error string `json:"error,omitempty"`
}
