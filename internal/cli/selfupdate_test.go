package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/install"
)

// stubBinary points resolveBinary at a scratch file standing in for the
// running executable, optionally with an installer receipt next to it.
func stubBinary(t *testing.T, receipt string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "positronick")
	if err := os.WriteFile(bin, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	if receipt != "" {
		if err := os.WriteFile(filepath.Join(dir, install.ReceiptName), []byte(receipt), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orig := resolveBinary
	resolveBinary = func() (string, error) { return bin, nil }
	t.Cleanup(func() { resolveBinary = orig })
	return bin
}

// releaseTarGz builds a goreleaser-shaped archive holding the new binary.
func releaseTarGz(t *testing.T, binaryContent string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "positronick", Mode: 0o755,
		Size: int64(len(binaryContent))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(binaryContent)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// stubReleases serves a GitHub-releases-shaped API for the running test
// platform and points POSITRONICK_RELEASES_API at it.
func stubReleases(t *testing.T, tag string, archive []byte) {
	t.Helper()
	asset := fmt.Sprintf("positronick_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%x  %s\n", sum, asset)

	mux := http.NewServeMux()
	var srv *httptest.Server
	release := func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[`+
			`{"name":%q,"browser_download_url":"%s/dl/%s"},`+
			`{"name":"checksums.txt","browser_download_url":"%s/dl/checksums.txt"}]}`,
			tag, asset, srv.URL, asset, srv.URL)
	}
	mux.HandleFunc("GET /repos/Positronick/cli/releases/latest", release)
	mux.HandleFunc("GET /repos/Positronick/cli/releases/tags/"+tag, release)
	mux.HandleFunc("GET /dl/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("GET /dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv(releasesAPIEnv, srv.URL)
}

// Without an installer receipt, self update never touches the binary or the
// network — it reports the method and the right upgrade command, exit 0.
func TestSelfUpdateUnknownMethod(t *testing.T) {
	bin := stubBinary(t, "")
	// No POSITRONICK_RELEASES_API: any lookup would dial api.github.com and
	// fail loud — passing proves no lookup happened.
	t.Setenv(releasesAPIEnv, "")

	stdout, stderr, code := executeAgainst(t, "", "self", "update", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "self-update-unknown.json", stdout)
	if got, _ := os.ReadFile(bin); string(got) != "OLD BINARY" {
		t.Errorf("binary = %q, must be untouched", got)
	}

	stdout, _, code = executeAgainst(t, "", "self", "update")
	if code != 0 {
		t.Fatalf("human exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "install.sh") {
		t.Errorf("stdout = %q, want the installer re-run suggested", stdout)
	}
}

// A Homebrew path gets `brew upgrade positronick`, never an in-place swap.
func TestSelfUpdateHomebrew(t *testing.T) {
	orig := resolveBinary
	resolveBinary = func() (string, error) {
		return "/opt/homebrew/Cellar/positronick/0.1.0/bin/positronick", nil
	}
	t.Cleanup(func() { resolveBinary = orig })

	stdout, stderr, code := executeAgainst(t, "", "self", "update", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "self-update-homebrew.json", stdout)

	stdout, _, code = executeAgainst(t, "", "self", "update")
	if code != 0 {
		t.Fatalf("human exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "brew upgrade positronick") {
		t.Errorf("stdout = %q, want the brew command", stdout)
	}
}

// --check reports without replacing anything.
func TestSelfUpdateCheck(t *testing.T) {
	bin := stubBinary(t, `{"method":"installer","version":"dev"}`)
	stubReleases(t, "v9.9.9", releaseTarGz(t, "NEW BINARY"))

	stdout, stderr, code := executeAgainst(t, "", "self", "update", "--check", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "self-update-check.json", stdout)
	if got, _ := os.ReadFile(bin); string(got) != "OLD BINARY" {
		t.Errorf("binary = %q, --check must never replace it", got)
	}
}

// The full installer-method update: download, verify, swap, rewrite receipt.
func TestSelfUpdateApplies(t *testing.T) {
	bin := stubBinary(t, `{"method":"installer","version":"dev"}`)
	stubReleases(t, "v9.9.9", releaseTarGz(t, "NEW BINARY"))

	stdout, stderr, code := executeAgainst(t, "", "self", "update", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "self-update-updated.json", stdout)

	if got, _ := os.ReadFile(bin); string(got) != "NEW BINARY" {
		t.Errorf("binary = %q, want the released content", got)
	}
	if r := install.ReadReceipt(bin); r.Version != "9.9.9" {
		t.Errorf("receipt = %+v, want version 9.9.9", r)
	}
}

// Matching versions mean no download and a friendly exit 0.
func TestSelfUpdateUpToDate(t *testing.T) {
	bin := stubBinary(t, `{"method":"installer","version":"dev"}`)
	// version.Version is "dev" in tests; tag "vdev" trims to the same.
	stubReleases(t, "vdev", releaseTarGz(t, "NEW BINARY"))

	stdout, stderr, code := executeAgainst(t, "", "self", "update", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "self-update-uptodate.json", stdout)
	if got, _ := os.ReadFile(bin); string(got) != "OLD BINARY" {
		t.Errorf("binary = %q, an up-to-date binary must not be replaced", got)
	}
}

// --version pins the release tag instead of latest.
func TestSelfUpdatePinnedVersion(t *testing.T) {
	bin := stubBinary(t, `{"method":"installer","version":"dev"}`)
	stubReleases(t, "v9.9.9", releaseTarGz(t, "PINNED BINARY"))

	_, stderr, code := executeAgainst(t, "", "self", "update", "--version", "v9.9.9", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got, _ := os.ReadFile(bin); string(got) != "PINNED BINARY" {
		t.Errorf("binary = %q, want the pinned release content", got)
	}
}
