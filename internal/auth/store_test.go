package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testCreds() *Credentials {
	return &Credentials{
		Version:     1,
		BaseURL:     "https://positronick.com",
		AccessToken: "tok-123",
		TokenType:   "Bearer",
		ExpiresAt:   "2026-06-17T09:00:00Z",
		User: StoredUser{
			ID:      "usr_1",
			Name:    "Ada Lovelace",
			Email:   "ada@example.com",
			IsAdmin: true,
		},
		CreatedAt: "2026-06-10T09:00:00Z",
	}
}

// captureWarnings swaps the store's warning writer for a buffer.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := warnWriter
	warnWriter = &buf
	t.Cleanup(func() { warnWriter = old })
	return &buf
}

// The credential cache must round-trip losslessly — including the cached
// isAdmin bit a later PR uses to reveal admin commands — and land with owner-
// only permissions: it holds a session token.
func TestSaveLoadRoundTrip(t *testing.T) {
	warnings := captureWarnings(t)
	dir := filepath.Join(t.TempDir(), "positronick")
	want := testCreds()

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "credentials.json"))
	if err != nil {
		t.Fatalf("credentials.json missing after Save: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 0600", perm)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 0700 (Save creates the config dir)", perm)
	}

	got, err := Load(dir, "https://positronick.com")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if warnings.Len() != 0 {
		t.Errorf("no warning expected for a 0600 file, got %q", warnings.String())
	}
}

// Save must be atomic — temp file + rename — so a crash mid-write never
// leaves a corrupt or half-written credential file (or a stray temp file).
func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, testCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "credentials.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("dir contents = %v, want exactly [credentials.json]", names)
	}
}

// Missing credentials are not an error — they mean anonymous.
func TestLoadMissing(t *testing.T) {
	got, err := Load(t.TempDir(), "https://positronick.com")
	if err != nil {
		t.Fatalf("Load on empty dir: %v", err)
	}
	if got != nil {
		t.Errorf("Load = %+v, want nil for missing file", got)
	}
}

// Credentials are keyed to the base URL: a production token must never be
// sent to localhost (or any other host), so a mismatch loads as anonymous.
func TestLoadOtherBaseURLIsNil(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, testCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir, "http://localhost:5173")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Errorf("Load = %+v, want nil when the cached baseUrl differs", got)
	}
}

// A group/other-readable credential file is repaired to 0600 with a one-line
// warning — the token inside is a live session.
func TestLoadFixesLoosePermissions(t *testing.T) {
	warnings := captureWarnings(t)
	dir := t.TempDir()
	if err := Save(dir, testCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(dir, "credentials.json")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir, "https://positronick.com")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || got.AccessToken != "tok-123" {
		t.Fatalf("Load = %+v, want the credentials despite the perms fix", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode after Load = %o, want 0600", perm)
	}
	warning := warnings.String()
	if warning == "" || !strings.Contains(warning, "credentials.json") {
		t.Errorf("warning = %q, want one line naming credentials.json", warning)
	}
	if lines := strings.Count(strings.TrimRight(warning, "\n"), "\n"); lines != 0 {
		t.Errorf("warning must be a single line, got %q", warning)
	}
}

// Fail-loud: a corrupt credential file surfaces, it is not silently treated
// as anonymous — `positronick logout` is the documented fix.
func TestLoadMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir, "https://positronick.com")
	if err == nil {
		t.Fatal("malformed credentials.json must produce an error")
	}
	if !strings.Contains(err.Error(), "credentials.json") {
		t.Errorf("error should name the offending file, got %q", err)
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, testCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Delete(dir); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.json")); !os.IsNotExist(err) {
		t.Errorf("credentials.json should be gone, stat err = %v", err)
	}
	// Logging out twice is fine — there is nothing to fail about.
	if err := Delete(dir); err != nil {
		t.Errorf("Delete with nothing cached = %v, want nil", err)
	}
}

// Guard the on-disk JSON key names: they are a compatibility contract with
// future CLI versions reading the same file.
func TestStoredJSONShape(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, testCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"version"`, `"baseUrl"`, `"accessToken"`, `"tokenType"`, `"expiresAt"`,
		`"user"`, `"id"`, `"name"`, `"email"`, `"isAdmin"`, `"createdAt"`,
	} {
		if !bytes.Contains(raw, []byte(key)) {
			t.Errorf("credentials.json missing key %s in %s", key, raw)
		}
	}
}
