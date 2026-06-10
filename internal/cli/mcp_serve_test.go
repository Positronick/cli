package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/mcpserver"
	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/version"
)

// newMCPSession connects an in-process MCP client to the embedded server,
// wired exactly like `mcp serve` wires it: the same API client construction
// and the same did-you-mean suggestion machinery (closestSlug).
func newMCPSession(t *testing.T, handler http.Handler, cwd, home string) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := api.New(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	server := mcpserver.New(mcpserver.Options{Client: client, Cwd: cwd, Home: home, Suggest: closestSlug})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := mcpClient.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callTool invokes one tool, failing the test on a protocol-level error —
// tool-level errors come back in the result and are asserted by each caller.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) protocol error: %v", name, err)
	}
	return res
}

// textOf returns the result's markdown text block.
func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

// structuredAs re-marshals the result's structured content into out, so tests
// assert the typed contract agents script against.
func structuredAs(t *testing.T, res *mcp.CallToolResult, out any) {
	t.Helper()
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("structured content does not match the expected shape: %v\n%s", err, b)
	}
}

// The initialize handshake advertises the binary's identity — name
// "positronick", version from internal/version — which MCP clients display
// and log.
func TestMCPServeHandshake(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	info := session.InitializeResult().ServerInfo
	if info.Name != "positronick" {
		t.Errorf("server name = %q, want %q", info.Name, "positronick")
	}
	if info.Version != version.Version {
		t.Errorf("server version = %q, want %q", info.Version, version.Version)
	}
}

// tools/list is the MCP public contract: exactly five consolidated tools with
// pinned names, descriptions and schemas. A golden diff here is a contract
// change and must be called out in the PR.
func TestMCPServeToolsListGolden(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 5 {
		t.Fatalf("tools/list returned %d tools, want exactly 5", len(res.Tools))
	}

	got, err := json.MarshalIndent(res.Tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "golden", "mcp-tools-list.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run `go test ./internal/cli -update`): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("tools/list does not match %s — a contract change?\ngot:\n%s\nwant:\n%s",
			path, got, want)
	}
}

// soul_search must rank exactly like `soul search` (the website's fuzzy
// relevance) and return the slim card subset — agents pay tokens for every
// field, so the full body must never travel in search results.
func TestMCPSoulSearch(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res := callTool(t, session, "soul_search", map[string]any{"query": "o"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}

	var out struct {
		Count int              `json:"count"`
		Souls []map[string]any `json:"souls"`
	}
	structuredAs(t, res, &out)
	if out.Count != 3 || len(out.Souls) != 3 {
		t.Fatalf("count = %d with %d souls, want 3 and 3", out.Count, len(out.Souls))
	}
	// Same ranking as `soul search o` (the pinned CLI golden).
	wantOrder := []string{"moriarty", "watson", "sherlock"}
	for i, want := range wantOrder {
		if got := out.Souls[i]["slug"]; got != want {
			t.Errorf("souls[%d].slug = %v, want %q", i, got, want)
		}
	}
	// The card subset, exactly: no content, no description, no rating noise.
	wantKeys := []string{"slug", "name", "tagline", "category", "frameworks", "version", "downloadCount"}
	if len(out.Souls[0]) != len(wantKeys) {
		t.Errorf("soul summary has %d fields, want %d (%v)", len(out.Souls[0]), len(wantKeys), out.Souls[0])
	}
	for _, k := range wantKeys {
		if _, ok := out.Souls[0][k]; !ok {
			t.Errorf("soul summary missing %q", k)
		}
	}

	text := textOf(t, res)
	for _, want := range []string{"3 souls", "moriarty", "watson", "sherlock"} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown block missing %q:\n%s", want, text)
		}
	}
}

// The category/framework facets and the limit mirror the CLI's flags; count
// reports what was returned, after the limit — the same contract as the CLI's
// --json output.
func TestMCPSoulSearchFiltersAndLimit(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())

	res := callTool(t, session, "soul_search",
		map[string]any{"query": "o", "framework": "hermes", "limit": 1})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}
	var out struct {
		Count int `json:"count"`
		Souls []struct {
			Slug string `json:"slug"`
		} `json:"souls"`
	}
	structuredAs(t, res, &out)
	// hermes filters moriarty (openclaw) out; watson outranks sherlock for
	// "o"; limit 1 keeps watson only.
	if out.Count != 1 || len(out.Souls) != 1 || out.Souls[0].Slug != "watson" {
		t.Errorf("got count %d, souls %v; want exactly watson", out.Count, out.Souls)
	}
}

// soul_show returns the full soul — SOUL.md body included — for the
// search → show → install funnel, plus a markdown block with a metadata
// header and the body.
func TestMCPSoulShow(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res := callTool(t, session, "soul_show", map[string]any{"slug": "sherlock"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}

	var out struct {
		Soul api.Soul `json:"soul"`
	}
	structuredAs(t, res, &out)
	if out.Soul.Slug != "sherlock" {
		t.Errorf("soul.slug = %q, want sherlock", out.Soul.Slug)
	}
	if out.Soul.Content != mockapi.Souls[0].Content {
		t.Errorf("soul.content = %q, want the verbatim body", out.Soul.Content)
	}

	text := textOf(t, res)
	for _, want := range []string{"Sherlock", "sherlock", "You are Sherlock."} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown block missing %q:\n%s", want, text)
		}
	}
}

// A missing slug is a tool error the agent can read and self-correct — with
// the same did-you-mean machinery the CLI uses — never a protocol error or a
// server crash.
func TestMCPSoulShowNotFound(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res := callTool(t, session, "soul_show", map[string]any{"slug": "sherloc"})
	if !res.IsError {
		t.Fatal("soul_show on a typo must return a tool error result")
	}
	text := textOf(t, res)
	for _, want := range []string{`soul "sherloc" not found`, `did you mean "sherlock"?`} {
		if !strings.Contains(text, want) {
			t.Errorf("error text missing %q:\n%s", want, text)
		}
	}

	// The session must survive the tool error.
	if err := session.Ping(t.Context(), nil); err != nil {
		t.Errorf("server did not survive a tool error: %v", err)
	}
}

// soul_install reuses the CLI's install machinery end to end: hermes is the
// fallback target, the body is fetched via the counter-bumping .md endpoint
// only after the overwrite gate passes, and the write is verbatim.
func TestMCPSoulInstall(t *testing.T) {
	cwd, home := t.TempDir(), t.TempDir()
	session := newMCPSession(t, mockapi.InstallHandler(), cwd, home)

	res := callTool(t, session, "soul_install", map[string]any{"slug": "sherlock"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}
	var out struct {
		Installed struct {
			Slug   string `json:"slug"`
			Target string `json:"target"`
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
		} `json:"installed"`
	}
	structuredAs(t, res, &out)
	wantPath := filepath.Join(home, ".hermes", "SOUL.md")
	if out.Installed.Slug != "sherlock" || out.Installed.Target != "hermes" || out.Installed.Path != wantPath {
		t.Errorf("installed = %+v, want sherlock/hermes at %s", out.Installed, wantPath)
	}
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("installed file missing: %v", err)
	}
	if string(body) != mockapi.Souls[0].Content {
		t.Errorf("installed body = %q, want the verbatim SOUL.md", body)
	}
	if out.Installed.Bytes != len(body) {
		t.Errorf("installed.bytes = %d, want %d", out.Installed.Bytes, len(body))
	}

	// Existing file: never prompts (no TTY) — an error result telling the
	// caller to pass a path or remove the file, leaving the file untouched.
	res = callTool(t, session, "soul_install", map[string]any{"slug": "sherlock"})
	if !res.IsError {
		t.Fatal("re-install over an existing file must return a tool error result")
	}
	text := textOf(t, res)
	for _, want := range []string{"already exists", "path", "remove"} {
		if !strings.Contains(text, want) {
			t.Errorf("overwrite error missing %q:\n%s", want, text)
		}
	}
}

// An explicit target uses that target's conventional path, exactly like
// `soul install --target`.
func TestMCPSoulInstallExplicitTarget(t *testing.T) {
	cwd, home := t.TempDir(), t.TempDir()
	session := newMCPSession(t, mockapi.InstallHandler(), cwd, home)

	res := callTool(t, session, "soul_install", map[string]any{"slug": "watson", "target": "claude"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}
	wantPath := filepath.Join(home, ".claude", "SOUL.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected install at %s: %v", wantPath, err)
	}
}

// Caller-supplied paths follow the documented safety rule: a relative path
// must resolve inside the user's home directory (no TTY to confirm escapes);
// an absolute path is explicit intent and is honored.
func TestMCPSoulInstallPathSafety(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	session := newMCPSession(t, mockapi.InstallHandler(), cwd, home)

	res := callTool(t, session, "soul_install",
		map[string]any{"slug": "sherlock", "path": "../../escaped/SOUL.md"})
	if !res.IsError {
		t.Fatal("a relative path escaping home must return a tool error result")
	}
	if text := textOf(t, res); !strings.Contains(text, "home") {
		t.Errorf("escape error should name the home-directory rule:\n%s", text)
	}

	// A relative path inside home is a bare verbatim write ("path" target).
	res = callTool(t, session, "soul_install",
		map[string]any{"slug": "sherlock", "path": "souls/SOUL.md"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}
	var out struct {
		Installed struct {
			Target string `json:"target"`
			Path   string `json:"path"`
		} `json:"installed"`
	}
	structuredAs(t, res, &out)
	wantPath := filepath.Join(cwd, "souls", "SOUL.md")
	if out.Installed.Target != "path" || out.Installed.Path != wantPath {
		t.Errorf("installed = %+v, want target \"path\" at %s", out.Installed, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected install at %s: %v", wantPath, err)
	}

	// An absolute path outside home is explicit intent.
	absPath := filepath.Join(t.TempDir(), "SOUL.md")
	res = callTool(t, session, "soul_install",
		map[string]any{"slug": "sherlock", "path": absPath})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}
	if _, err := os.Stat(absPath); err != nil {
		t.Errorf("expected install at %s: %v", absPath, err)
	}
}

// listing_search scopes by type server-side (?type=) and ranks like
// `<type> search`, returning the slim subset with the official install
// command and source URL.
func TestMCPListingSearch(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res := callTool(t, session, "listing_search", map[string]any{"query": "git", "type": "cli"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}

	var out struct {
		Count    int              `json:"count"`
		Listings []map[string]any `json:"listings"`
	}
	structuredAs(t, res, &out)
	if out.Count != 1 || len(out.Listings) != 1 {
		t.Fatalf("count = %d with %d listings, want 1 and 1", out.Count, len(out.Listings))
	}
	l := out.Listings[0]
	if l["slug"] != "github-cli" || l["type"] != "cli" {
		t.Errorf("listing = %v, want github-cli (cli)", l)
	}
	if l["installCmd"] != "brew install gh" {
		t.Errorf("installCmd = %v, want the official install command", l["installCmd"])
	}
	if l["sourceUrl"] != "https://example.com/cli" {
		t.Errorf("sourceUrl = %v, want the verified source", l["sourceUrl"])
	}

	if text := textOf(t, res); !strings.Contains(text, "github-cli") {
		t.Errorf("markdown block missing github-cli:\n%s", text)
	}
}

// Without a type, listing_search ranks across the whole registry.
func TestMCPListingSearchAllTypes(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res := callTool(t, session, "listing_search", map[string]any{"query": "loop"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}
	var out struct {
		Count    int `json:"count"`
		Listings []struct {
			Slug string `json:"slug"`
		} `json:"listings"`
	}
	structuredAs(t, res, &out)
	if out.Count != 1 || out.Listings[0].Slug != "pr-to-green" {
		t.Errorf("got %+v, want exactly pr-to-green", out)
	}
}

// listing_show returns the full listing; for loops the markdown block renders
// the recipe and the kickoff prompt — the entire point of a loop listing.
func TestMCPListingShowLoop(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res := callTool(t, session, "listing_show", map[string]any{"slug": "pr-to-green"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(t, res))
	}

	var out struct {
		Listing api.Listing `json:"listing"`
	}
	structuredAs(t, res, &out)
	if out.Listing.Slug != "pr-to-green" || out.Listing.Type != "loop" {
		t.Errorf("listing = %s (%s), want pr-to-green (loop)", out.Listing.Slug, out.Listing.Type)
	}
	if out.Listing.Data["kickoff"] == nil {
		t.Error("structured listing must keep the loop data payload")
	}

	text := textOf(t, res)
	for _, want := range []string{
		"Kickoff",
		"Run the pr-to-green loop on the open PR:", // the kickoff prompt itself
		"gh pr checks --json state",                // the loop's check command
	} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown block missing %q:\n%s", want, text)
		}
	}
}

// listing_show's not-found carries the did-you-mean hint, like the CLI's.
func TestMCPListingShowNotFound(t *testing.T) {
	session := newMCPSession(t, mockapi.Handler(), t.TempDir(), t.TempDir())
	res := callTool(t, session, "listing_show", map[string]any{"slug": "pr-to-gren"})
	if !res.IsError {
		t.Fatal("listing_show on a typo must return a tool error result")
	}
	text := textOf(t, res)
	for _, want := range []string{`listing "pr-to-gren" not found`, `did you mean "pr-to-green"?`} {
		if !strings.Contains(text, want) {
			t.Errorf("error text missing %q:\n%s", want, text)
		}
	}
}

// `mcp serve` on a terminal must not hijack the session with JSON-RPC: it
// prints ready-to-paste setup instructions and exits 0. The golden pins that
// human contract.
func TestMCPServeInstructionsGolden(t *testing.T) {
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = orig })

	stdout, stderr, code := executeAgainst(t, "", "mcp", "serve")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	path := filepath.Join("testdata", "golden", "mcp-serve-instructions.txt")
	if *update {
		if err := os.WriteFile(path, []byte(stdout), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run `go test ./internal/cli -update`): %v", err)
	}
	if stdout != string(want) {
		t.Errorf("instructions do not match %s — a contract change?\ngot:\n%s\nwant:\n%s",
			path, stdout, want)
	}
}
