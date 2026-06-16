package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/auth"
	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// ── pure helpers ─────────────────────────────────────────────────────────────

// Frontmatter splitting is the foundation of `soul create --file`: the YAML
// block must come apart from the body losslessly, on LF and CRLF files alike,
// and a broken fence must fail loud instead of posting garbage as a soul.
func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantYAML string
		wantBody string
		wantErr  bool
	}{
		{
			name:     "no frontmatter is all body",
			raw:      "# SOUL.md\n\nJust a body.\n",
			wantBody: "# SOUL.md\n\nJust a body.\n",
		},
		{
			name:     "frontmatter splits from body",
			raw:      "---\nname: Owl\ntags: [a, b]\n---\n\n# Identity\n",
			wantYAML: "name: Owl\ntags: [a, b]\n",
			wantBody: "\n# Identity\n",
		},
		{
			name:     "CRLF line endings",
			raw:      "---\r\nname: Owl\r\n---\r\nBody line.\r\n",
			wantYAML: "name: Owl\r\n",
			wantBody: "Body line.\r\n",
		},
		{
			name:     "closing fence at EOF leaves an empty body",
			raw:      "---\nname: Owl\n---",
			wantYAML: "name: Owl\n",
			wantBody: "",
		},
		{
			name:    "unterminated fence is an error",
			raw:     "---\nname: Owl\nno closing fence\n",
			wantErr: true,
		},
		{
			name:     "a fence mid-body is not frontmatter",
			raw:      "intro\n---\nname: Owl\n---\n",
			wantBody: "intro\n---\nname: Owl\n---\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlText, body, err := splitFrontmatter(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want an error for an unterminated fence")
				}
				return
			}
			if err != nil {
				t.Fatalf("splitFrontmatter: %v", err)
			}
			if yamlText != tt.wantYAML {
				t.Errorf("yaml = %q, want %q", yamlText, tt.wantYAML)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

// parseSoulFile must hand the frontmatter to the server verbatim except for
// the server-assigned fields — a file copied straight out of
// src/content/souls (which carries id/createdAt/updatedAt) must just work.
func TestParseSoulFileDropsServerAssignedFields(t *testing.T) {
	raw := "---\nid: 01SOMETHING\nslug: owl\nname: Owl\nframeworks: [hermes]\n" +
		"createdAt: 2026-06-07\nupdatedAt: 2026-06-07\n---\n\n# Identity\n"
	fields, err := parseSoulFile("owl.md", raw)
	if err != nil {
		t.Fatalf("parseSoulFile: %v", err)
	}
	for _, dropped := range []string{"id", "createdAt", "updatedAt"} {
		if _, ok := fields[dropped]; ok {
			t.Errorf("field %q must be dropped — it is server-assigned", dropped)
		}
	}
	if fields["slug"] != "owl" || fields["name"] != "Owl" {
		t.Errorf("fields = %v, want frontmatter values passed through", fields)
	}
	if fields["content"] != "\n# Identity\n" {
		t.Errorf("content = %q, want the body verbatim", fields["content"])
	}
}

func TestParseSoulFileBadYAML(t *testing.T) {
	if _, err := parseSoulFile("x.md", "---\n[not: a: map\n---\nbody\n"); err == nil {
		t.Fatal("want an error for unparseable frontmatter")
	}
}

// ULID detection decides whether a 404'd update argument is retried as a
// slug. Crockford base32 excludes I, L, O, U — most slugs (hyphens,
// lowercase words) and even the fixture's fake ids fail the check.
func TestIsULID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", true},
		{"01arz3ndektsv4rrffq69g5fav", true}, // case-insensitive
		{"01SCREATED0000000000000001", true},
		{"01SOULSHERLOCK0000000000XX", false}, // O, U, L are not Crockford
		{"01ARZ3NDEKTSV4RRFFQ69G5FA", false},  // 25 chars
		{"01ARZ3NDEKTSV4RRFFQ69G5FAVX", false},
		{"pr-to-green-pr-to-green-26", false}, // hyphens
		{"", false},
	}
	for _, tt := range tests {
		if got := isULID(tt.in); got != tt.want {
			t.Errorf("isULID(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// ── reveal-on-admin ──────────────────────────────────────────────────────────

// findPath walks the command tree by names, failing the test on a miss.
func findPath(t *testing.T, root *cobra.Command, names ...string) *cobra.Command {
	t.Helper()
	c := root
	for _, name := range names {
		c = findCommand(c, name)
		if c == nil {
			t.Fatalf("command path %v not found", names)
		}
	}
	return c
}

var adminCommandPaths = [][]string{
	{"soul", "create"},
	{"soul", "update"},
	{"listing"},
	{"listing", "create"},
	{"listing", "update"},
	{"loop", "create"},
	{"profile"},
	{"profile", "create"},
	{"profile", "list"},
}

// The admin commands are hidden by default and flip visible from the CACHED
// isAdmin bit — a pure local read, no network: help must answer instantly
// and offline.
func TestRevealAdminCommands(t *testing.T) {
	seedAdminCache := func(t *testing.T, isAdmin bool) {
		t.Helper()
		dir := t.TempDir()
		t.Setenv("POSITRONICK_CONFIG_DIR", dir)
		err := auth.Save(dir, &auth.Credentials{
			Version: 1, BaseURL: "https://positronick.com", AccessToken: "tok-1",
			User: auth.StoredUser{ID: "usr_1", Name: "A", Email: "a@example.com", IsAdmin: isAdmin},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Run("hidden when logged out", func(t *testing.T) {
		t.Setenv("POSITRONICK_CONFIG_DIR", t.TempDir())
		root := NewRootCmd()
		for _, path := range adminCommandPaths {
			if !findPath(t, root, path...).Hidden {
				t.Errorf("%v must be hidden without a cached admin login", path)
			}
		}
	})

	t.Run("hidden for a non-admin", func(t *testing.T) {
		seedAdminCache(t, false)
		if !findPath(t, NewRootCmd(), "soul", "create").Hidden {
			t.Error("soul create must stay hidden for a non-admin login")
		}
	})

	t.Run("revealed for a cached admin", func(t *testing.T) {
		seedAdminCache(t, true)
		root := NewRootCmd()
		for _, path := range adminCommandPaths {
			if findPath(t, root, path...).Hidden {
				t.Errorf("%v must be revealed for a cached admin", path)
			}
		}
	})

	t.Run("a corrupt cache reveals nothing and breaks nothing", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("POSITRONICK_CONFIG_DIR", dir)
		if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte("{corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Construction must not panic or error: logout (the documented fix
		// for a corrupt cache) has to keep working.
		if !findPath(t, NewRootCmd(), "soul", "create").Hidden {
			t.Error("a corrupt cache must not reveal admin commands")
		}
	})

	t.Run("agent-docs reflect what the current user sees", func(t *testing.T) {
		t.Setenv("POSITRONICK_CONFIG_DIR", t.TempDir())
		stdout, _, code := executeAgainst(t, "", "agent-docs")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if strings.Contains(stdout, "positronick soul create") {
			t.Error("agent-docs must not document admin commands for a non-admin")
		}
		if !strings.Contains(stdout, "Hidden admin commands") {
			t.Error("the agent-docs intro must mention that hidden admin commands exist")
		}

		seedAdminCache(t, true)
		stdout, _, code = executeAgainst(t, "", "agent-docs")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		for _, want := range []string{
			"positronick soul create", "positronick soul update",
			"positronick listing create", "positronick loop create",
			"positronick profile create", "positronick profile list",
		} {
			if !strings.Contains(stdout, "## "+want) {
				t.Errorf("agent-docs for an admin must document %q", want)
			}
		}
	})
}

// ── CLI-level, against the mock admin API ────────────────────────────────────

// adminEnv authenticates the invocation as the fixture admin via the API key
// path (env > cache, sent as x-api-key).
func adminEnv(t *testing.T) {
	t.Helper()
	isolateAuth(t)
	t.Setenv("POSITRONICK_API_KEY", mockapi.AdminAPIKey)
}

// lastWrite records the most recent mutating request the mock received.
type lastWrite struct {
	method, path string
	body         []byte
}

// newCaptureServer wraps the mock API, recording every POST/PATCH so tests
// can assert the exact JSON the CLI put on the wire.
func newCaptureServer(t *testing.T) (*httptest.Server, *lastWrite) {
	t.Helper()
	last := &lastWrite{}
	inner := mockapi.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("reading request body: %v", err)
			}
			last.method, last.path, last.body = r.Method, r.URL.Path, b
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, last
}

// soul create from a SOUL.md fixture: 201, with the --json shape and the
// human summary (public path included) pinned as goldens.
func TestSoulCreateGolden(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "create", "--file", "testdata/new-soul.md", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "soul-create.json", stdout)
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t) // fresh state: the slug is free again
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "create", "--file", "testdata/new-soul.md")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "soul-create.txt", stdout)
		if !strings.Contains(stdout, "/souls/night-owl") {
			t.Errorf("stdout = %q, want the public URL path", stdout)
		}
	})
}

// Flags override frontmatter, the server-assigned fields are stripped, and
// the body rides as content — asserted on the exact JSON the mock received.
func TestSoulCreateFlagsOverrideFrontmatter(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	_, stderr, code := executeAgainst(t, srv.URL,
		"soul", "create", "--file", "testdata/new-soul.md",
		"--name", "Day Owl", "--slug", "day-owl", "--framework", "cursor", "--status", "draft", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if last.method != http.MethodPost || last.path != "/api/admin/souls" {
		t.Fatalf("request = %s %s, want POST /api/admin/souls", last.method, last.path)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	if sent["name"] != "Day Owl" || sent["slug"] != "day-owl" || sent["status"] != "draft" {
		t.Errorf("sent = %v, want the flag values to beat the frontmatter", sent)
	}
	if !reflect.DeepEqual(sent["frameworks"], []any{"cursor"}) {
		t.Errorf("frameworks = %v, want the --framework override", sent["frameworks"])
	}
	for _, dropped := range []string{"id", "createdAt", "updatedAt"} {
		if _, ok := sent[dropped]; ok {
			t.Errorf("sent body must not carry server-assigned %q", dropped)
		}
	}
	if c, _ := sent["content"].(string); !strings.Contains(c, "# Identity") {
		t.Errorf("content = %q, want the markdown body", c)
	}
	// Untouched frontmatter passes through.
	if sent["authorHandle"] != "nsollazzo" || sent["license"] != "MIT" {
		t.Errorf("sent = %v, want unflagged frontmatter fields verbatim", sent)
	}
}

// A 422 is the seed validator speaking: its message must reach the user
// verbatim, in the envelope and as the human error line.
func TestSoulCreate422Verbatim(t *testing.T) {
	const msg = `admin soul create: missing required field "tagline"`

	t.Run("json envelope", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "create", "--file", "testdata/invalid-soul.md", "--json")
		if code != output.ExitError {
			t.Fatalf("exit code = %d, want %d", code, output.ExitError)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty on error", stdout)
		}
		want := `{"error":{"code":"invalid_input","message":"admin soul create: missing required field \"tagline\""}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		_, stderr, code := executeAgainst(t, srv.URL,
			"soul", "create", "--file", "testdata/invalid-soul.md")
		if code != output.ExitError {
			t.Fatalf("exit code = %d, want %d", code, output.ExitError)
		}
		if stderr != "error: "+msg+"\n" {
			t.Errorf("stderr = %q, want the validator message verbatim", stderr)
		}
	})
}

// 401 (no credentials) and 403 (authenticated non-admin) both map to exit 4
// with the admin-login hint — the server is the authorization authority, the
// CLI just relays its envelope.
func TestAdminAuthErrors(t *testing.T) {
	t.Run("401 logged out", func(t *testing.T) {
		isolateAuth(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "create", "--file", "testdata/new-soul.md", "--json")
		if code != output.ExitAuth {
			t.Fatalf("exit code = %d, want %d", code, output.ExitAuth)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty on error", stdout)
		}
		want := `{"error":{"code":"auth_required","message":"authentication required — run ` +
			"`positronick login`" + `","hint":"run positronick login with an admin account"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("403 non-admin", func(t *testing.T) {
		dir := isolateAuth(t)
		srv := newMockServer(t)
		seedLogin(t, dir, srv.URL, mockapi.PlebToken)
		_, stderr, code := executeAgainst(t, srv.URL,
			"soul", "create", "--file", "testdata/new-soul.md", "--json")
		if code != output.ExitAuth {
			t.Fatalf("exit code = %d, want %d", code, output.ExitAuth)
		}
		want := `{"error":{"code":"auth_required","message":"admin access required",` +
			`"hint":"run positronick login with an admin account"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

// Updating a seed-owned soul by SLUG: the ref 404s as an id, resolves through
// the public detail endpoint, and the patch flips ownership — tookOwnership
// in the JSON contract, a WARNING on stderr for humans.
func TestSoulUpdateBySlugTookOwnership(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "update", "sherlock", "--tagline", "Deduce, then debug", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "soul-update-ownership.json", stdout)
		if !strings.Contains(stdout, `"tookOwnership": true`) {
			t.Errorf("stdout = %q, want tookOwnership true for a seed-owned row", stdout)
		}
	})

	t.Run("human warning", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "update", "sherlock", "--tagline", "Deduce, then debug")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "soul-update-ownership.txt", stdout)
		if !strings.Contains(stderr, "WARNING: took ownership from the seed") {
			t.Errorf("stderr = %q, want the ownership warning", stderr)
		}
		if !strings.Contains(stderr, "--source seed") {
			t.Errorf("stderr = %q, want the hand-back hint", stderr)
		}
	})

	t.Run("explicit --source seed does not take ownership", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "update", "sherlock", "--tagline", "tweak", "--source", "seed")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if strings.Contains(stderr, "WARNING") {
			t.Errorf("stderr = %q, want no warning when ownership stays with the seed", stderr)
		}
		if !strings.Contains(stdout, "Updated soul sherlock") {
			t.Errorf("stdout = %q, want the update summary", stdout)
		}
	})
}

// An update by id PATCHes directly — no slug resolution round-trip — and a
// ref that is neither is exit 3.
func TestSoulUpdateResolution(t *testing.T) {
	t.Run("by id", func(t *testing.T) {
		adminEnv(t)
		srv, last := newCaptureServer(t)
		_, stderr, code := executeAgainst(t, srv.URL,
			"soul", "update", "01SOULWATSON000000000000XX", "--tagline", "Steady hands", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if last.path != "/api/admin/souls/01SOULWATSON000000000000XX" {
			t.Errorf("path = %q, want the id PATCHed directly", last.path)
		}
		if string(last.body) != `{"tagline":"Steady hands"}` {
			t.Errorf("body = %s, want only the provided field", last.body)
		}
	})

	t.Run("unknown ref is exit 3", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"soul", "update", "no-such-soul", "--tagline", "x", "--json")
		if code != output.ExitNotFound {
			t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty on error", stdout)
		}
		if !strings.Contains(stderr, `"code":"not_found"`) ||
			!strings.Contains(stderr, `soul \"no-such-soul\" not found`) {
			t.Errorf("stderr = %q, want the not-found envelope", stderr)
		}
	})

	t.Run("no fields is a client-side error", func(t *testing.T) {
		adminEnv(t)
		// Unreachable server: the validation must fire before any request.
		_, stderr, code := executeAgainst(t, "http://127.0.0.1:1", "soul", "update", "sherlock")
		if code != output.ExitError {
			t.Fatalf("exit code = %d, want %d", code, output.ExitError)
		}
		if !strings.Contains(stderr, "nothing to update") {
			t.Errorf("stderr = %q, want the nothing-to-update error", stderr)
		}
	})
}

// listing create from a content-shaped JSON file: id and official are
// stripped, --profile injects the handle, and the --json shape is pinned.
func TestListingCreate(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"listing", "create", "--file", "testdata/new-listing.json", "--profile", "nsollazzo", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "listing-create.json", stdout)

	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	for _, dropped := range []string{"id", "official"} {
		if _, ok := sent[dropped]; ok {
			t.Errorf("sent body must not carry %q", dropped)
		}
	}
	if sent["profileHandle"] != "nsollazzo" {
		t.Errorf("profileHandle = %v, want the --profile flag value", sent["profileHandle"])
	}
	if sent["slug"] != "sentry-mcp" || sent["type"] != "mcp" {
		t.Errorf("sent = %v, want the file fields verbatim", sent)
	}
}

// Without a resolvable profile the server refuses: profiles stay git-curated.
func TestListingCreateUnknownProfile(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)
	_, stderr, code := executeAgainst(t, srv.URL,
		"listing", "create", "--file", "testdata/new-listing.json", "--profile", "nobody", "--json")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d", code, output.ExitError)
	}
	want := `{"error":{"code":"unknown_profile","message":"profile \"nobody\" does not exist"}}` + "\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// loop create is sugar: the flags must assemble exactly the type=loop listing
// body the server expects, LoopData keys included.
func TestLoopCreateBuildsBody(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"loop", "create",
		"--name", "Docs to Green",
		"--slug", "docs-to-green",
		"--goal", "Every doc page builds without warnings",
		"--check-command", "make docs",
		"--exit-condition", "make docs exits 0 twice in a row",
		"--max-iterations", "20",
		"--kickoff", "Run the docs-to-green loop until the build is clean.",
		"--profile", "nsollazzo",
		"--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "loop-create.json", stdout)

	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	want := map[string]any{
		"slug":          "docs-to-green",
		"name":          "Docs to Green",
		"type":          "loop",
		"tagline":       "Every doc page builds without warnings", // defaults to the goal
		"category":      "AI/ML",                                  // default
		"sourceUrl":     "https://positronick.com/registry/loop",  // default
		"profileHandle": "nsollazzo",
		"data": map[string]any{
			"goal":          "Every doc page builds without warnings",
			"checkCommand":  "make docs",
			"exitCondition": "make docs exits 0 twice in a row",
			"maxIterations": float64(20),
			"kickoff":       "Run the docs-to-green loop until the build is clean.",
		},
	}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("sent body = %#v, want %#v", sent, want)
	}
}

func TestLoopCreateKickoffFile(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)
	kickoffPath := filepath.Join(t.TempDir(), "kickoff.md")
	if err := os.WriteFile(kickoffPath, []byte("Start the loop.\nIterate until green.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := executeAgainst(t, srv.URL,
		"loop", "create", "--name", "L", "--slug", "kickoff-loop", "--goal", "g",
		"--check-command", "c", "--exit-condition", "e", "--profile", "nsollazzo",
		"--kickoff-file", kickoffPath, "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var sent struct {
		Data struct {
			Kickoff string `json:"kickoff"`
		} `json:"data"`
	}
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Data.Kickoff != "Start the loop.\nIterate until green.\n" {
		t.Errorf("kickoff = %q, want the file content verbatim", sent.Data.Kickoff)
	}
}

// There is no delete verb: unpublishing is PATCH {"status":"draft"}, by slug.
func TestUnpublishViaStatusDraft(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"listing", "update", "pr-to-green", "--status", "draft", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if last.path != "/api/admin/listings/01LSTPRTOGREEN0000000000XX" {
		t.Errorf("path = %q, want the slug resolved to the listing id", last.path)
	}
	if string(last.body) != `{"status":"draft"}` {
		t.Errorf("body = %s, want exactly the status patch", last.body)
	}
	assertGolden(t, "listing-unpublish.json", stdout)
	if !strings.Contains(stdout, `"status": "draft"`) {
		t.Errorf("stdout = %q, want the listing unpublished", stdout)
	}
}
