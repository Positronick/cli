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
