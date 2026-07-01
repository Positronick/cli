// Package mcpserver embeds an MCP (Model Context Protocol) stdio server in
// the positronick binary: five consolidated tools — soul_search, soul_show,
// soul_install, listing_search, listing_show — each a thin wrapper over the
// same internal packages the CLI commands use (the api client, the search
// ranking, the install machinery), so the two surfaces can never disagree.
// The MCP SDK dependency is confined to this package; the `mcp serve`
// command in internal/cli is just glue.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/version"
)

// defaultLimit caps search-tool results when the caller omits limit. Tool
// results land in a model's context window, so it is deliberately smaller
// than the CLI's default of 20.
const defaultLimit = 10

// Options wires the server's dependencies. Everything is injected — the
// auth-aware API client the CLI builds, the cwd/home anchors for installs,
// and the did-you-mean suggester — so this package never imports the CLI and
// tests control every seam.
type Options struct {
	// Client is the positronick.com API client every tool reads through.
	Client *api.Client
	// Cwd and Home anchor harness detection, the conventional install paths
	// and soul_install's relative-path safety rule (see internal/install).
	Cwd, Home string
	// Suggest picks the did-you-mean candidate for a missing slug, or ""
	// for none. The CLI injects its suggestion machinery; nil disables
	// suggestions.
	Suggest func(input string, candidates []string) string
}

// New builds the MCP server advertising the five positronick tools. The
// server name is "positronick"; the version is the binary's build version;
// the initialize result carries serverInstructions for the client's model.
func New(opts Options) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "positronick", Version: version.Version},
		&mcp.ServerOptions{Instructions: serverInstructions})
	addSoulTools(srv, opts)
	addListingTools(srv, opts)
	return srv
}

// Run serves New(opts) over stdio (newline-delimited JSON-RPC on
// stdin/stdout) until the client disconnects or ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	return New(opts).Run(ctx, &mcp.StdioTransport{})
}

// textResult wraps a compact markdown rendering as the tool result's
// unstructured content; the typed output value supplies the structured half.
func textResult(markdown string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: markdown}}}
}

// inputSchema infers the JSON schema for In from its struct tags and applies
// the patches tag inference cannot express (enums, minimums, defaults). A
// failure here is a programmer error in a type literal, so it panics — the
// same posture as the SDK's AddTool, and any test building the server
// catches it.
func inputSchema[In any](patch func(s *jsonschema.Schema)) *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(err)
	}
	if patch != nil {
		patch(s)
	}
	return s
}

// limitSchema constrains a search tool's limit property: at least 1, and the
// SDK fills in the default when the caller omits it.
func limitSchema(s *jsonschema.Schema) {
	one := float64(1)
	s.Minimum = &one
	s.Default = fmt.Appendf(nil, "%d", defaultLimit)
}

// enumOf renders string values as a JSON-schema enum list.
func enumOf(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// capLimit truncates ranked items to the caller's limit (≤ 0 means the
// schema default was bypassed; fall back to defaultLimit rather than crash).
func capLimit[T any](items []T, limit int) []T {
	if limit <= 0 {
		limit = defaultLimit
	}
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

// countNoun renders "3 souls" / "1 soul" for markdown headers.
func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// writeField appends one "- label: value" markdown line, dropping empty
// values so detail blocks never show blank fields.
func writeField(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "- %s: %s\n", label, value)
}

// handleLabel renders "@handle (Full Name)", or just "@handle" without a name.
func handleLabel(handle, name string) string {
	if name == "" {
		return "@" + handle
	}
	return fmt.Sprintf("@%s (%s)", handle, name)
}

// deref renders an optional string field, mapping nil to "".
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// notFoundErr builds the tool-error text for a missing slug: `<noun> "slug"
// not found`, upgraded to a did-you-mean when opts.Suggest finds a plausible
// neighbor among the slugs fetchSlugs returns. A failing suggestion fetch
// never masks the original not-found. noun doubles as the search tool's name
// (`<noun>_search`) in the browse hint.
func notFoundErr(ctx context.Context, opts Options, slug, noun string, fetchSlugs func(context.Context) ([]string, error)) error {
	msg := fmt.Sprintf("%s %q not found", noun, slug)
	if opts.Suggest != nil {
		if slugs, err := fetchSlugs(ctx); err == nil {
			if match := opts.Suggest(slug, slugs); match != "" {
				return fmt.Errorf("%s — did you mean %q? (browse with %s_search)", msg, match, noun)
			}
		}
	}
	return fmt.Errorf("%s (browse with %s_search)", msg, noun)
}
