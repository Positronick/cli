// Package selfupdate implements `positronick self update`: it classifies how
// the binary was installed (only the installer receipt authorizes an
// in-place replace), talks to the GitHub releases API, verifies the
// downloaded archive against checksums.txt, and swaps the binary atomically.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/positronick/cli/internal/install"
)

// DefaultAPIBase is the GitHub REST API the release lookups go to.
const DefaultAPIBase = "https://api.github.com"

// DefaultRepo is the owner/name of the CLI's release repository.
const DefaultRepo = "Positronick/cli"

// DetectMethod classifies the install method. The receipt is authoritative
// when it says "installer"; otherwise the resolved binary path is matched
// against homebrew, npm and go-install conventions (goBin is the caller's
// resolved go-install bin directory, "" when unknown). Everything else is
// "unknown" and treated like a package-manager install.
func DetectMethod(receiptMethod, binPath, goBin string) string {
	if receiptMethod == "installer" {
		return "installer"
	}
	p := filepath.ToSlash(binPath)
	switch {
	case strings.Contains(p, "/Cellar/") || strings.Contains(p, "/homebrew/") ||
		strings.Contains(p, "/.linuxbrew/"):
		return "homebrew"
	case strings.Contains(p, "/node_modules/"):
		return "npm"
	case goBin != "" && filepath.Dir(binPath) == filepath.Clean(goBin):
		return "go"
	default:
		return "unknown"
	}
}

// UpgradeCommand returns the copy-pasteable upgrade command for a
// non-installer method ("" only for "installer" itself).
func UpgradeCommand(method string) string {
	switch method {
	case "homebrew":
		return "brew upgrade positronick"
	case "npm":
		return "npm install -g positronick@latest"
	case "go":
		return "go install github.com/positronick/cli/cmd/positronick@latest"
	case "unknown":
		return "curl -fsSL https://positronick.com/install.sh | sh"
	default:
		return ""
	}
}

// SameVersion reports whether the current build version and a release tag
// name the same version, ignoring any leading "v" (goreleaser injects
// "0.2.0", tags are "v0.2.0").
func SameVersion(current, tag string) bool {
	return strings.TrimPrefix(current, "v") == strings.TrimPrefix(tag, "v")
}

// Release is the subset of the GitHub release object the updater needs.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Updater performs release lookups and the in-place binary swap. APIBase is
// injectable so tests run against httptest instead of api.github.com.
type Updater struct {
	// APIBase is the GitHub REST API base URL (DefaultAPIBase in production).
	APIBase string
	// Repo is the owner/name release repository.
	Repo string
	// HTTP is the client used for every request; nil gets a 30s-timeout default.
	HTTP *http.Client
	// BinPath is the resolved (symlink-free) path of the running binary.
	BinPath string
	// GOOS and GOARCH pick the release asset, e.g. positronick_linux_amd64.tar.gz.
	GOOS, GOARCH string
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Latest fetches the most recent release.
func (u *Updater) Latest(ctx context.Context) (*Release, error) {
	return u.release(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", u.APIBase, u.Repo))
}

// ByTag fetches one release by tag; a missing "v" prefix is added so both
// "0.2.0" and "v0.2.0" work.
func (u *Updater) ByTag(ctx context.Context, tag string) (*Release, error) {
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return u.release(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", u.APIBase, u.Repo, tag))
}

// release fetches and decodes one GitHub release object.
func (u *Updater) release(ctx context.Context, url string) (*Release, error) {
	body, err := u.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("decoding release from %s: %w", url, err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("release from %s has no tag_name", url)
	}
	return &rel, nil
}

// get performs one GET and returns the body; any non-200 status is an error.
func (u *Updater) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "positronick-cli-selfupdate")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("GET %s: reading body: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return body, nil
}

// Apply downloads the platform archive and checksums.txt from rel, verifies
// the archive's sha256 against the published checksum, extracts the binary,
// and replaces BinPath atomically (a temp file in the same directory renamed
// into place, mode 0755). On success the install receipt next to the binary
// is rewritten with the new version. Verification failures leave the
// existing binary and receipt untouched.
func (u *Updater) Apply(ctx context.Context, rel *Release) error {
	assetName := fmt.Sprintf("positronick_%s_%s.tar.gz", u.GOOS, u.GOARCH)
	asset, err := findAsset(rel, assetName)
	if err != nil {
		return err
	}
	checksums, err := findAsset(rel, "checksums.txt")
	if err != nil {
		return err
	}

	archive, err := u.get(ctx, asset.BrowserDownloadURL, "application/octet-stream")
	if err != nil {
		return err
	}
	sums, err := u.get(ctx, checksums.BrowserDownloadURL, "application/octet-stream")
	if err != nil {
		return err
	}
	if err := verifyChecksum(archive, sums, assetName); err != nil {
		return err
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return fmt.Errorf("extracting %s: %w", assetName, err)
	}
	if err := replaceBinary(u.BinPath, binary); err != nil {
		return err
	}
	return install.WriteReceipt(u.BinPath, install.Receipt{
		Method:  "installer",
		Version: strings.TrimPrefix(rel.TagName, "v"),
	})
}

// findAsset locates one release asset by exact name.
func findAsset(rel *Release, name string) (*Asset, error) {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("release %s has no asset %q", rel.TagName, name)
}

// verifyChecksum checks archive against the "<sha256>  <name>" line for
// assetName in the goreleaser checksums.txt body.
func verifyChecksum(archive, checksums []byte, assetName string) error {
	var expected string
	for line := range strings.Lines(string(checksums)) {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", assetName)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(archive))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s — refusing to install",
			assetName, expected, actual)
	}
	return nil
}

// extractBinary pulls the "positronick" file out of the tar.gz archive.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("archive does not contain the positronick binary")
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == "positronick" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

// replaceBinary writes the new binary to a temp file in the same directory
// as binPath (so the final rename is atomic on the same filesystem), chmods
// it 0755 and renames it over the old binary.
func replaceBinary(binPath string, binary []byte) error {
	dir := filepath.Dir(binPath)
	tmp, err := os.CreateTemp(dir, ".positronick-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, binPath); err != nil {
		return fmt.Errorf("replacing %s: %w", binPath, err)
	}
	return nil
}
