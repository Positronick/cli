package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/auth"
	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
)

// mockHost replaces the mock server's random-port URL in golden output so the
// files stay byte-stable across runs.
const mockHost = "https://mock.positronick.test"

// isolateAuth points the CLI at a fresh config dir and clears any developer
// POSITRONICK_API_KEY so auth tests are hermetic.
func isolateAuth(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("POSITRONICK_CONFIG_DIR", dir)
	t.Setenv("POSITRONICK_API_KEY", "")
	return dir
}

// seedLogin caches a device-flow login for baseURL, as `positronick login`
// would have left it.
func seedLogin(t *testing.T, dir, baseURL, token string) {
	t.Helper()
	err := auth.Save(dir, &auth.Credentials{
		Version:     1,
		BaseURL:     baseURL,
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresAt:   "2026-06-17T09:00:00Z",
		User: auth.StoredUser{
			ID: "usr_mock0001", Name: "Stale Name", Email: "stale@example.com", IsAdmin: true,
		},
		CreatedAt: "2026-06-10T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("seeding credentials: %v", err)
	}
}

// assertGolden compares got (with the live server URL normalized away)
// against testdata/golden/<name>, honoring -update like TestGolden does.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run `go test ./internal/cli -update`): %v", err)
	}
	if got != string(want) {
		t.Errorf("output does not match %s — a contract change?\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

// login --json is an NDJSON stream: exactly two lines, each valid JSON — the
// pending line immediately (so callers can surface the code), the
// authenticated line after approval. This is a scripted public contract.
func TestLoginJSONStream(t *testing.T) {
	dir := isolateAuth(t)
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "login", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty in JSON mode", stderr)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout = %q, want exactly 2 NDJSON lines", stdout)
	}
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %q", i, line)
		}
	}
	assertGolden(t, "login.ndjson", strings.ReplaceAll(stdout, srv.URL, mockHost))

	// The login must land in the credential cache, keyed to the base URL,
	// with owner-only permissions.
	stored, err := auth.Load(dir, srv.URL)
	if err != nil {
		t.Fatalf("Load after login: %v", err)
	}
	if stored == nil || stored.AccessToken != mockapi.AccessToken {
		t.Fatalf("stored = %+v, want the granted session token", stored)
	}
	if stored.User.Name != "Ada Lovelace" || stored.User.IsAdmin {
		t.Errorf("stored user = %+v, want the /api/me identity with isAdmin false", stored.User)
	}
	info, err := os.Stat(filepath.Join(dir, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials.json mode = %o, want 0600", perm)
	}
}

// Human login prints the dashed code and the plain verification URL (the
// user may be reading this over SSH and typing on a phone), then confirms
// the identity; the waiting line is progress, so it goes to stderr.
func TestLoginHuman(t *testing.T) {
	isolateAuth(t)
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "login")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"First, copy this code: TJSP-LLAV\n",
		"Then visit: " + srv.URL + "/device\n",
		"Logged in as Ada Lovelace (ada@example.com)\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "user_code=") {
		t.Errorf("stdout = %q, must print the plain URI, not the complete one", stdout)
	}
	if !strings.Contains(stderr, "Waiting for approval") {
		t.Errorf("stderr = %q, want the waiting status line", stderr)
	}
}

// loginErrorServer speaks just enough device flow to fail polling with the
// given OAuth error code.
func loginErrorServer(t *testing.T, oauthCode string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"device_code":"dev-1","user_code":"TJSPLLAV",`+
			`"verification_uri":"http://%s/device",`+
			`"verification_uri_complete":"http://%s/device?user_code=TJSPLLAV",`+
			`"expires_in":1800,"interval":0}`, r.Host, r.Host)
	})
	mux.HandleFunc("POST /api/auth/device/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":%q}`, oauthCode)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A denial exits 2 (cancelled), an expired code exits 4 (auth) — the exit
// codes scripts branch on.
func TestLoginDeniedAndExpired(t *testing.T) {
	tests := []struct {
		name      string
		oauthCode string
		wantExit  int
		wantErr   string
	}{
		{"denied", "access_denied", output.ExitCancelled, "authorization denied"},
		{"expired", "expired_token", output.ExitAuth, "run positronick login again"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateAuth(t)
			srv := loginErrorServer(t, tt.oauthCode)
			stdout, stderr, code := executeAgainst(t, srv.URL, "login", "--json")
			if code != tt.wantExit {
				t.Errorf("exit code = %d, want %d", code, tt.wantExit)
			}
			// The pending line was already streamed; the failure goes to
			// stderr as the JSON envelope.
			if !strings.Contains(stdout, `"status":"pending"`) {
				t.Errorf("stdout = %q, want the pending NDJSON line", stdout)
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantErr)
			}
		})
	}
}

func TestLogout(t *testing.T) {
	dir := isolateAuth(t)
	seedLogin(t, dir, "https://positronick.com", "tok-1")

	stdout, _, code := executeAgainst(t, "", "logout", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	assertGolden(t, "logout.json", stdout)
	if _, err := os.Stat(filepath.Join(dir, "credentials.json")); !os.IsNotExist(err) {
		t.Errorf("credentials.json should be deleted, stat err = %v", err)
	}

	// Logging out with nothing cached is fine, not an error.
	stdout, _, code = executeAgainst(t, "", "logout")
	if code != 0 {
		t.Errorf("repeat logout exit code = %d, want 0", code)
	}
	if stdout != "Logged out.\n" {
		t.Errorf("stdout = %q, want %q", stdout, "Logged out.\n")
	}
}

// auth status is a query, not an assertion: every resolvable state — logged
// out, live device session, live API key, stale credentials — exits 0 with a
// pinned JSON shape.
func TestAuthStatusGolden(t *testing.T) {
	t.Run("logged out", func(t *testing.T) {
		isolateAuth(t)
		// An unreachable base URL proves no network call happens without creds.
		stdout, stderr, code := executeAgainst(t, mockHost, "auth", "status", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "auth-status-loggedout.json", stdout)
	})

	t.Run("device session", func(t *testing.T) {
		dir := isolateAuth(t)
		srv := newMockServer(t)
		seedLogin(t, dir, srv.URL, mockapi.AccessToken)
		stdout, stderr, code := executeAgainst(t, srv.URL, "auth", "status", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "auth-status-device.json", strings.ReplaceAll(stdout, srv.URL, mockHost))
	})

	t.Run("api key", func(t *testing.T) {
		isolateAuth(t)
		srv := newMockServer(t)
		t.Setenv("POSITRONICK_API_KEY", mockapi.APIKey)
		stdout, stderr, code := executeAgainst(t, srv.URL, "auth", "status", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "auth-status-apikey.json", strings.ReplaceAll(stdout, srv.URL, mockHost))
	})

	t.Run("stale credentials still exit 0", func(t *testing.T) {
		dir := isolateAuth(t)
		srv := newMockServer(t)
		seedLogin(t, dir, srv.URL, "no-longer-valid")
		stdout, stderr, code := executeAgainst(t, srv.URL, "auth", "status", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "auth-status-stale.json", strings.ReplaceAll(stdout, srv.URL, mockHost))
	})

	t.Run("human layout", func(t *testing.T) {
		dir := isolateAuth(t)
		srv := newMockServer(t)
		seedLogin(t, dir, srv.URL, mockapi.AccessToken)
		stdout, _, code := executeAgainst(t, srv.URL, "auth", "status")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		assertGolden(t, "auth-status.txt", strings.ReplaceAll(stdout, srv.URL, mockHost))
	})
}

// A successful live check refreshes the cached identity and isAdmin bit —
// the cache must track the server, not the login-time snapshot.
func TestAuthStatusRefreshesCache(t *testing.T) {
	dir := isolateAuth(t)
	srv := newMockServer(t)
	seedLogin(t, dir, srv.URL, mockapi.AccessToken) // cached as Stale Name, isAdmin true

	if _, stderr, code := executeAgainst(t, srv.URL, "auth", "status", "--json"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	stored, err := auth.Load(dir, srv.URL)
	if err != nil || stored == nil {
		t.Fatalf("Load after status: %v, %+v", err, stored)
	}
	if stored.User.Name != "Ada Lovelace" || stored.User.Email != "ada@example.com" {
		t.Errorf("cached user = %+v, want it refreshed from /api/me", stored.User)
	}
	if stored.User.IsAdmin {
		t.Error("cached isAdmin must be refreshed to the server's value (false)")
	}
	if stored.AccessToken != mockapi.AccessToken {
		t.Errorf("token = %q, must be preserved by the refresh", stored.AccessToken)
	}
}

// Minting a key needs a session: the server refuses API-key credentials for
// key creation, so the CLI fails fast with exit 4 before any network call.
func TestTokenCreateRequiresLogin(t *testing.T) {
	t.Run("nothing cached", func(t *testing.T) {
		isolateAuth(t)
		stdout, stderr, code := executeAgainst(t, mockHost, "auth", "token", "create", "--json")
		if code != output.ExitAuth {
			t.Errorf("exit code = %d, want %d", code, output.ExitAuth)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty on error", stdout)
		}
		if !strings.Contains(stderr, `"code":"auth_required"`) || !strings.Contains(stderr, "login") {
			t.Errorf("stderr = %q, want an auth_required envelope hinting at login", stderr)
		}
	})

	t.Run("env API key only", func(t *testing.T) {
		isolateAuth(t)
		t.Setenv("POSITRONICK_API_KEY", mockapi.APIKey)
		_, stderr, code := executeAgainst(t, mockHost, "auth", "token", "create", "--json")
		if code != output.ExitAuth {
			t.Errorf("exit code = %d, want %d", code, output.ExitAuth)
		}
		if !strings.Contains(stderr, "API key cannot create API keys") {
			t.Errorf("stderr = %q, want the api-key restriction explained", stderr)
		}
	})
}

func TestTokenCreateJSON(t *testing.T) {
	dir := isolateAuth(t)
	srv := newMockServer(t)
	seedLogin(t, dir, srv.URL, mockapi.AccessToken)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"auth", "token", "create", "--name", "ci-bot", "--expires-days", "30", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "auth-token-create.json", stdout)
}

// Human mode prints the raw key alone on stdout — pipe-safe for
// POSITRONICK_API_KEY=$(positronick auth token create) — with the one-time
// warning and usage hint on stderr.
func TestTokenCreateHuman(t *testing.T) {
	dir := isolateAuth(t)
	srv := newMockServer(t)
	seedLogin(t, dir, srv.URL, mockapi.AccessToken)

	stdout, stderr, code := executeAgainst(t, srv.URL, "auth", "token", "create")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != mockapi.APIKey+"\n" {
		t.Errorf("stdout = %q, want exactly the raw key on its own line", stdout)
	}
	for _, want := range []string{"never be shown again", "export POSITRONICK_API_KEY="} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

// --expires-days is validated client-side (the server caps at 365 too) so
// the failure is immediate and names the valid range.
func TestTokenCreateExpiresDaysValidation(t *testing.T) {
	dir := isolateAuth(t)
	seedLogin(t, dir, mockHost, "tok-1")
	for _, days := range []string{"366", "-1"} {
		_, stderr, code := executeAgainst(t, mockHost, "auth", "token", "create", "--expires-days", days)
		if code != output.ExitError {
			t.Errorf("--expires-days %s exit code = %d, want %d", days, code, output.ExitError)
		}
		if !strings.Contains(stderr, "between 1 and 365") {
			t.Errorf("--expires-days %s stderr = %q, want the valid range named", days, stderr)
		}
	}
}

// Every command authenticates through the provider now: a cached login is
// sent as a bearer on plain read commands, and POSITRONICK_API_KEY beats it.
func TestReadCommandsUseProvider(t *testing.T) {
	dir := isolateAuth(t)

	var gotBearer, gotKey string
	inner := mockapi.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/souls" {
			gotBearer = r.Header.Get("Authorization")
			gotKey = r.Header.Get("x-api-key")
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	seedLogin(t, dir, srv.URL, mockapi.AccessToken)

	if _, stderr, code := executeAgainst(t, srv.URL, "soul", "list", "--json"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if gotBearer != "Bearer "+mockapi.AccessToken {
		t.Errorf("Authorization = %q, want the cached bearer", gotBearer)
	}

	t.Setenv("POSITRONICK_API_KEY", "posi_env_wins")
	if _, stderr, code := executeAgainst(t, srv.URL, "soul", "list", "--json"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if gotKey != "posi_env_wins" || gotBearer != "" {
		t.Errorf("headers = (key %q, bearer %q), want the env key to win and suppress the bearer",
			gotKey, gotBearer)
	}
}

func TestFormatUserCode(t *testing.T) {
	tests := []struct{ in, want string }{
		{"TJSPLLAV", "TJSP-LLAV"},  // the server's dash-less 8-char code
		{"ABCD-EFGH", "ABCD-EFGH"}, // already dashed: untouched
		{"ABC", "ABC"},             // unexpected length: passed through verbatim
	}
	for _, tt := range tests {
		if got := formatUserCode(tt.in); got != tt.want {
			t.Errorf("formatUserCode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The browser only auto-opens for a real person with a display: interactive
// TTY, a graphical environment, and no --no-browser.
func TestShouldOpenBrowser(t *testing.T) {
	interactive := output.Mode{TTY: true}
	tests := []struct {
		name             string
		mode             output.Mode
		noBrowser        bool
		goos             string
		display, wayland string
		want             bool
	}{
		{"interactive macOS", interactive, false, "darwin", "", "", true},
		{"interactive linux with X11", interactive, false, "linux", ":0", "", true},
		{"interactive linux with wayland", interactive, false, "linux", "", "wayland-0", true},
		{"interactive headless linux", interactive, false, "linux", "", "", false},
		{"--no-browser wins", interactive, true, "darwin", "", "", false},
		{"non-TTY never opens", output.Mode{}, false, "darwin", "", "", false},
		{"CI never opens", output.Mode{TTY: true, CI: true}, false, "darwin", "", "", false},
		{"coding agent never opens", output.Mode{TTY: true, Agent: "claude-code"}, false, "darwin", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldOpenBrowser(tt.mode, tt.noBrowser, tt.goos, tt.display, tt.wayland)
			if got != tt.want {
				t.Errorf("shouldOpenBrowser = %v, want %v", got, tt.want)
			}
		})
	}
}
