package main

// Release assets are a cross-repo contract: .goreleaser.yaml produces
// positronick_<os>_<arch>.tar.gz (windows: .zip) plus checksums.txt, and both
// installers — install.sh in this repo and the served copy in
// mi-dika/positronick (src/lib/installScript.ts) — download assets by those
// exact names, as does internal/selfupdate. This test derives its
// expectations from the selfupdate exports (the Go consumer of the contract)
// and pins .goreleaser.yaml and install.sh against them, so a rename anywhere
// in this repo fails CI instead of breaking `curl | sh` or self-update for
// users. The served installer lives in another repo and cannot be covered
// here.

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/positronick/cli/internal/install"
	"github.com/positronick/cli/internal/selfupdate"
)

type goreleaserConfig struct {
	Archives []struct {
		NameTemplate    string   `yaml:"name_template"`
		Formats         []string `yaml:"formats"`
		FormatOverrides []struct {
			Goos    string   `yaml:"goos"`
			Formats []string `yaml:"formats"`
		} `yaml:"format_overrides"`
	} `yaml:"archives"`
	Checksum struct {
		NameTemplate string `yaml:"name_template"`
	} `yaml:"checksum"`
}

func TestGoreleaserMatchesInstallerContract(t *testing.T) {
	// goreleaser appends the archive format itself, so the template is the
	// asset name minus the extension.
	wantArchiveTemplate := strings.TrimSuffix(selfupdate.AssetName("{{ .Os }}", "{{ .Arch }}"), ".tar.gz")

	raw, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}
	var cfg goreleaserConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse .goreleaser.yaml: %v", err)
	}

	if len(cfg.Archives) != 1 {
		t.Fatalf("expected exactly 1 archive config, got %d", len(cfg.Archives))
	}
	archive := cfg.Archives[0]
	if archive.NameTemplate != wantArchiveTemplate {
		t.Errorf("archive name_template = %q, want %q (installer contract)", archive.NameTemplate, wantArchiveTemplate)
	}
	if len(archive.Formats) != 1 || archive.Formats[0] != "tar.gz" {
		t.Errorf("archive formats = %v, want [tar.gz] (installer contract)", archive.Formats)
	}

	windowsZip := false
	for _, o := range archive.FormatOverrides {
		if o.Goos == "windows" && len(o.Formats) == 1 && o.Formats[0] == "zip" {
			windowsZip = true
		}
	}
	if !windowsZip {
		t.Errorf("missing windows zip format_override, want goos: windows -> formats: [zip]")
	}

	if cfg.Checksum.NameTemplate != selfupdate.ChecksumsName {
		t.Errorf("checksum name_template = %q, want %q (installer contract)", cfg.Checksum.NameTemplate, selfupdate.ChecksumsName)
	}
}

func TestInstallScriptMatchesInstallerContract(t *testing.T) {
	raw, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	script := string(raw)

	for _, want := range []string{
		// The shell-side rendering of the asset name.
		`ASSET="` + selfupdate.AssetName("${OS}", "${ARCH}") + `"`,
		// Anchored to the actual download, not a comment mentioning the name.
		`fetch "$BASE/` + selfupdate.ChecksumsName + `"`,
		// The receipt selfupdate reads to authorize in-place updates.
		`"$INSTALL_DIR/` + install.ReceiptName + `"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install.sh does not contain %q (installer contract)", want)
		}
	}
}
