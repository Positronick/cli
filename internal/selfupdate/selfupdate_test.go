package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/install"
)

// SameVersion decides "already up to date": the comparison must ignore the v
// prefix because goreleaser injects "0.2.0" while release tags are "v0.2.0".
func TestSameVersion(t *testing.T) {
	tests := []struct {
		current, tag string
		want         bool
	}{
		{"0.2.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", true},
		{"0.2.0", "0.2.0", true},
		{"0.2.0", "v0.3.0", false},
		{"dev", "v0.2.0", false},
	}
	for _, tt := range tests {
		if got := SameVersion(tt.current, tt.tag); got != tt.want {
			t.Errorf("SameVersion(%q, %q) = %v, want %v", tt.current, tt.tag, got, tt.want)
		}
	}
}

// DetectMethod routes `self update`: only the installer receipt authorizes an
// in-place replace; package-manager installs are recognized by path so the
// right upgrade command can be suggested instead of clobbering managed files.
func TestDetectMethod(t *testing.T) {
	tests := []struct {
		name          string
		receiptMethod string
		binPath       string
		goBin         string
		want          string
	}{
		{"receipt wins", "installer", "/home/u/.local/bin/positronick", "", "installer"},
		{"homebrew cellar", "unknown", "/opt/homebrew/Cellar/positronick/0.1.0/bin/positronick", "", "homebrew"},
		{"linuxbrew", "unknown", "/home/linuxbrew/.linuxbrew/bin/positronick", "", "homebrew"},
		{"npm node_modules", "unknown", "/usr/lib/node_modules/@positronick/cli-darwin-arm64/bin/positronick", "", "npm"},
		{"go install bin", "unknown", "/home/u/go/bin/positronick", "/home/u/go/bin", "go"},
		{"plain path", "unknown", "/home/u/.local/bin/positronick", "/home/u/go/bin", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectMethod(tt.receiptMethod, tt.binPath, tt.goBin); got != tt.want {
				t.Errorf("DetectMethod = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every non-installer method must come with a copy-pasteable upgrade command.
func TestUpgradeCommandCoversEveryMethod(t *testing.T) {
	for _, method := range []string{"homebrew", "npm", "go", "unknown"} {
		if cmd := UpgradeCommand(method); cmd == "" {
			t.Errorf("UpgradeCommand(%q) is empty", method)
		}
	}
	if !strings.Contains(UpgradeCommand("homebrew"), "brew upgrade positronick") {
		t.Errorf("homebrew command = %q, want brew upgrade positronick", UpgradeCommand("homebrew"))
	}
}

// tarGz builds a goreleaser-shaped release archive containing the binary.
func tarGz(t *testing.T, binaryContent string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body string
		mode int64
	}{
		{"LICENSE", "license text", 0o644},
		{"README.md", "readme", 0o644},
		{"positronick", binaryContent, 0o755},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releaseServer serves a GitHub-releases-shaped API: the latest-release JSON
// (with asset URLs pointing back at itself) plus the archive and checksum
// assets. checksum lets the test corrupt the published hash.
func releaseServer(t *testing.T, tag string, archive []byte, checksum string) *httptest.Server {
	t.Helper()
	asset := fmt.Sprintf("positronick_%s_%s.tar.gz", "linux", "amd64")
	checksums := fmt.Sprintf("%s  %s\n", checksum, asset)

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
	return srv
}

// scratchBinary stands in for the running executable: Apply must replace it
// in place, atomically, keeping 0755.
func scratchBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "positronick")
	if err := os.WriteFile(bin, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func newUpdater(binPath, apiBase string) *Updater {
	return &Updater{
		APIBase: apiBase,
		Repo:    "Positronick/cli",
		BinPath: binPath,
		GOOS:    "linux",
		GOARCH:  "amd64",
	}
}

func TestLatestAndByTag(t *testing.T) {
	archive := tarGz(t, "NEW BINARY v2")
	sum := sha256.Sum256(archive)
	srv := releaseServer(t, "v0.2.0", archive, fmt.Sprintf("%x", sum))
	u := newUpdater(scratchBinary(t), srv.URL)

	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.TagName != "v0.2.0" || len(rel.Assets) != 2 {
		t.Errorf("Latest = %+v, want tag v0.2.0 with 2 assets", rel)
	}

	rel, err = u.ByTag(context.Background(), "v0.2.0")
	if err != nil {
		t.Fatalf("ByTag: %v", err)
	}
	if rel.TagName != "v0.2.0" {
		t.Errorf("ByTag tag = %q, want v0.2.0", rel.TagName)
	}

	// The v prefix is added when missing — users type both forms.
	if _, err = u.ByTag(context.Background(), "0.2.0"); err != nil {
		t.Errorf("ByTag without v prefix: %v", err)
	}
}

// The successful path: download, verify, atomically swap the binary (mode
// preserved at 0755) and rewrite the receipt so the next update sees the new
// version.
func TestApplyReplacesBinaryAndReceipt(t *testing.T) {
	archive := tarGz(t, "NEW BINARY v2")
	sum := sha256.Sum256(archive)
	srv := releaseServer(t, "v0.2.0", archive, fmt.Sprintf("%x", sum))
	bin := scratchBinary(t)
	u := newUpdater(bin, srv.URL)

	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if err := u.Apply(context.Background(), rel); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW BINARY v2" {
		t.Errorf("binary = %q, want the released content", got)
	}
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("binary mode = %o, want 0755", perm)
	}
	receipt := install.ReadReceipt(bin)
	if receipt.Method != "installer" || receipt.Version != "0.2.0" {
		t.Errorf("receipt = %+v, want installer/0.2.0", receipt)
	}
	// No temp droppings left next to the binary.
	entries, err := os.ReadDir(filepath.Dir(bin))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 { // positronick + receipt
		t.Errorf("dir has %d entries, want 2 (binary + receipt): %v", len(entries), entries)
	}
}

// A checksum mismatch is the security gate: the corrupted download must be
// rejected and the running binary left byte-identical.
func TestApplyChecksumMismatchLeavesBinary(t *testing.T) {
	archive := tarGz(t, "EVIL BINARY")
	srv := releaseServer(t, "v0.2.0", archive, strings.Repeat("0", 64))
	bin := scratchBinary(t)
	u := newUpdater(bin, srv.URL)

	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	err = u.Apply(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Apply err = %v, want a checksum mismatch error", err)
	}
	got, readErr := os.ReadFile(bin)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "OLD BINARY" {
		t.Errorf("binary = %q, must be untouched after a failed verification", got)
	}
	if r := install.ReadReceipt(bin); r.Method != "unknown" {
		t.Errorf("receipt = %+v, must not be written on failure", r)
	}
}

// A release without the platform's asset fails loud with the asset named.
func TestApplyMissingAsset(t *testing.T) {
	archive := tarGz(t, "x")
	sum := sha256.Sum256(archive)
	srv := releaseServer(t, "v0.2.0", archive, fmt.Sprintf("%x", sum))
	u := newUpdater(scratchBinary(t), srv.URL)
	u.GOARCH = "riscv64"

	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	err = u.Apply(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), "positronick_linux_riscv64.tar.gz") {
		t.Errorf("Apply err = %v, want the missing asset named", err)
	}
}

// A 404 from the releases API surfaces as an error, not a zero release.
func TestLatestHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	u := newUpdater(scratchBinary(t), srv.URL)
	if _, err := u.Latest(context.Background()); err == nil {
		t.Fatal("Latest must fail on a non-200 response")
	}
}
