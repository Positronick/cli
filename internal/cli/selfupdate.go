package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/positronick/cli/internal/install"
	"github.com/positronick/cli/internal/output"
	"github.com/positronick/cli/internal/selfupdate"
	"github.com/positronick/cli/internal/version"
	"github.com/spf13/cobra"
)

// selfUpdateResult is the `self update --json` contract. Latest is null when
// no release lookup happened (non-installer methods).
type selfUpdateResult struct {
	Current string  `json:"current"`
	Latest  *string `json:"latest"`
	Updated bool    `json:"updated"`
	Method  string  `json:"method"`
}

// releasesAPIEnv overrides the GitHub releases API base URL — a test seam,
// like POSITRONICK_BASE_URL for the product API.
const releasesAPIEnv = "POSITRONICK_RELEASES_API"

// resolveBinary returns the symlink-resolved path of the running executable
// (the receipt and any in-place replace both key off the real file, not the
// pck alias). A package var so tests point it at a scratch binary.
var resolveBinary = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolving executable symlinks: %w", err)
	}
	return resolved, nil
}

func newSelfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage this positronick installation",
	}
	cmd.AddCommand(newSelfUpdateCmd())
	return cmd
}

const selfUpdateLong = `Update the positronick binary in place.

Only binaries placed by the installer (install.sh leaves a
positronick-install-receipt.json next to the binary) are replaced in place:
the latest GitHub release is downloaded, verified against checksums.txt and
swapped atomically. For package-manager installs (Homebrew, npm, go install)
the right upgrade command is printed instead — exit code 0 either way.`

func newSelfUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the positronick binary to the latest release",
		Long:  selfUpdateLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			check, err := cmd.Flags().GetBool("check")
			if err != nil {
				return err
			}
			pinned, err := cmd.Flags().GetString("version")
			if err != nil {
				return err
			}
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}

			binPath, err := resolveBinary()
			if err != nil {
				return err
			}
			receipt := install.ReadReceipt(binPath)
			method := selfupdate.DetectMethod(receipt.Method, binPath, goBinDir())
			current := version.Version

			if method != "installer" {
				res := selfUpdateResult{Current: current, Method: method}
				if p.Mode.JSON {
					return p.EmitJSON(res)
				}
				if method == "unknown" {
					p.Human("positronick was not installed by install.sh — update by re-running the installer:\n  %s\n",
						selfupdate.UpgradeCommand(method))
				} else {
					p.Human("positronick was installed via %s — update with:\n  %s\n",
						method, selfupdate.UpgradeCommand(method))
				}
				return nil
			}

			apiBase := os.Getenv(releasesAPIEnv)
			if apiBase == "" {
				apiBase = selfupdate.DefaultAPIBase
			}
			updater := &selfupdate.Updater{
				APIBase: apiBase,
				Repo:    selfupdate.DefaultRepo,
				BinPath: binPath,
				GOOS:    runtime.GOOS,
				GOARCH:  runtime.GOARCH,
			}

			var rel *selfupdate.Release
			if pinned != "" {
				rel, err = updater.ByTag(cmd.Context(), pinned)
			} else {
				rel, err = updater.Latest(cmd.Context())
			}
			if err != nil {
				return err
			}
			latest := rel.TagName
			res := selfUpdateResult{Current: current, Latest: &latest, Method: method}

			if selfupdate.SameVersion(current, latest) {
				if p.Mode.JSON {
					return p.EmitJSON(res)
				}
				p.Human("positronick %s is up to date.\n", current)
				return nil
			}
			if check {
				if p.Mode.JSON {
					return p.EmitJSON(res)
				}
				p.Human("Update available: %s → %s\n", current, latest)
				p.Status("Run: positronick self update\n")
				return nil
			}

			p.Status("Downloading positronick %s…\n", latest)
			if err := updater.Apply(cmd.Context(), rel); err != nil {
				return output.Errorf("%s", err)
			}
			res.Updated = true
			if p.Mode.JSON {
				return p.EmitJSON(res)
			}
			p.Human("Updated positronick %s → %s\n", current, latest)
			return nil
		},
	}
	cmd.Flags().Bool("check", false, "only report whether an update is available")
	cmd.Flags().String("version", "", "update to this release tag (e.g. v0.2.0) instead of latest")
	return cmd
}

// goBinDir resolves where `go install` puts binaries: $GOBIN, else
// $GOPATH/bin, else ~/go/bin ("" when home is unknown).
func goBinDir() string {
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		return gobin
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		return filepath.Join(gopath, "bin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "go", "bin")
}
