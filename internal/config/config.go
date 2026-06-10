// Package config resolves the CLI's configuration directory and settings.
// Resolution is layered — flag > environment > config file > default — and
// every layer is validated, never silently skipped.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultBaseURL is the production API used when nothing overrides it.
const DefaultBaseURL = "https://positronick.com"

// Dir returns the CLI's configuration directory:
// $POSITRONICK_CONFIG_DIR > $XDG_CONFIG_HOME/positronick > ~/.config/positronick.
// The directory is not created.
func Dir() (string, error) {
	if dir := os.Getenv("POSITRONICK_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "positronick"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving config dir: %w", err)
	}
	return filepath.Join(home, ".config", "positronick"), nil
}

// fileConfig is the on-disk shape of <config dir>/config.json.
type fileConfig struct {
	BaseURL string `json:"base_url"`
}

// ResolveBaseURL resolves the API base URL with precedence
// flag > $POSITRONICK_BASE_URL > config.json base_url > DefaultBaseURL.
// Whichever layer wins is validated (scheme must be http or https) and
// normalized (trailing slash stripped). A missing config file is fine; a
// malformed one is an error — fail loud, never guess.
func ResolveBaseURL(flagValue string) (string, error) {
	if flagValue != "" {
		return normalizeBaseURL(flagValue)
	}
	if env := os.Getenv("POSITRONICK_BASE_URL"); env != "" {
		return normalizeBaseURL(env)
	}

	fromFile, err := baseURLFromFile()
	if err != nil {
		return "", err
	}
	if fromFile != "" {
		return normalizeBaseURL(fromFile)
	}
	return DefaultBaseURL, nil
}

// baseURLFromFile reads base_url from <config dir>/config.json. A missing
// file returns ""; an unreadable or malformed file is an error.
func baseURLFromFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "config.json")

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	var cfg fileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg.BaseURL, nil
}

// normalizeBaseURL validates the scheme and strips any trailing slash.
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf(
			"invalid base URL %q: scheme must be http or https (e.g. %s)", raw, DefaultBaseURL)
	}
	return strings.TrimRight(u.String(), "/"), nil
}
