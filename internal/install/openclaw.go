package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/positronick/cli/internal/output"
)

// OpenClawWorkspace is one configured OpenClaw agent: its id and the
// workspace directory it reads SOUL.md from.
type OpenClawWorkspace struct {
	ID   string
	Path string
}

// openClawConfig mirrors the subset of openclaw.json this resolver reads.
// agents.list (the legacy array form) and agentDir/sandboxes are ignored per
// the PR-1 spec: a file that parses but has no agents.entries is treated as
// the single-agent case.
type openClawConfig struct {
	Agents struct {
		Defaults struct {
			Workspace string `json:"workspace"`
		} `json:"defaults"`
		Entries map[string]openClawAgentEntry `json:"entries"`
	} `json:"agents"`
}

type openClawAgentEntry struct {
	Default   bool   `json:"default"`
	Workspace string `json:"workspace"`
}

// ResolveOpenClawWorkspaces returns every configured OpenClaw agent and the
// workspace directory it reads SOUL.md from, sorted with the default agent
// first, then by id. home, getenv and readFile are injected so this is
// testable without touching the real filesystem/environment.
func ResolveOpenClawWorkspaces(home string, getenv func(string) string, readFile func(string) ([]byte, error)) ([]OpenClawWorkspace, error) {
	stateDir := openClawStateDir(home, getenv)
	configPath := filepath.Join(stateDir, "openclaw.json")

	body, err := readFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []OpenClawWorkspace{{ID: "main", Path: openClawDefaultWorkspace(stateDir, "", home, getenv)}}, nil
		}
		return nil, output.ErrorWithHint(
			fmt.Sprintf("cannot parse %s: %v", configPath, err),
			"pass --workspace <dir> to skip detection")
	}

	var cfg openClawConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, output.ErrorWithHint(
			fmt.Sprintf("cannot parse %s: %v", configPath, err),
			"pass --workspace <dir> to skip detection")
	}

	if len(cfg.Agents.Entries) == 0 {
		path := openClawDefaultWorkspace(stateDir, cfg.Agents.Defaults.Workspace, home, getenv)
		return []OpenClawWorkspace{{ID: "main", Path: path}}, nil
	}

	defaultID := openClawDefaultAgentID(cfg.Agents.Entries)
	workspaces := make([]OpenClawWorkspace, 0, len(cfg.Agents.Entries))
	for id, entry := range cfg.Agents.Entries {
		var path string
		switch {
		case entry.Workspace != "":
			path = expandHome(entry.Workspace, home)
		case id == defaultID:
			// agents.defaults.workspace is a shared root for the
			// non-default entries only — it does not bind the default
			// agent without an explicit agents.entries.<id>.workspace.
			path = openClawDefaultWorkspace(stateDir, "", home, getenv)
		case cfg.Agents.Defaults.Workspace != "":
			path = filepath.Join(expandHome(cfg.Agents.Defaults.Workspace, home), id)
		default:
			path = filepath.Join(stateDir, "workspace-"+id)
		}
		workspaces = append(workspaces, OpenClawWorkspace{ID: id, Path: path})
	}

	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].ID == defaultID {
			return true
		}
		if workspaces[j].ID == defaultID {
			return false
		}
		return workspaces[i].ID < workspaces[j].ID
	})
	return workspaces, nil
}

// openClawStateDir resolves $OPENCLAW_STATE_DIR, else ~/.openclaw-<profile>
// when $OPENCLAW_PROFILE is set and not "default", else ~/.openclaw.
func openClawStateDir(home string, getenv func(string) string) string {
	if dir := getenv("OPENCLAW_STATE_DIR"); dir != "" {
		return dir
	}
	if profile := getenv("OPENCLAW_PROFILE"); profile != "" && profile != "default" {
		return filepath.Join(home, ".openclaw-"+profile)
	}
	return filepath.Join(home, ".openclaw")
}

// openClawDefaultWorkspace resolves the default agent's workspace when it
// has no explicit `workspace` of its own: $OPENCLAW_WORKSPACE_DIR, else
// defaultsWorkspace (agents.defaults.workspace) if set, else
// <stateDir>/workspace.
func openClawDefaultWorkspace(stateDir, defaultsWorkspace, home string, getenv func(string) string) string {
	if dir := getenv("OPENCLAW_WORKSPACE_DIR"); dir != "" {
		return dir
	}
	if defaultsWorkspace != "" {
		return expandHome(defaultsWorkspace, home)
	}
	return filepath.Join(stateDir, "workspace")
}

// openClawDefaultAgentID picks the default agent id: the entry flagged
// `default: true`, else the entry named "main".
func openClawDefaultAgentID(entries map[string]openClawAgentEntry) string {
	for id, entry := range entries {
		if entry.Default {
			return id
		}
	}
	return "main"
}

// expandHome expands a leading "~/" to home; anything else is returned as-is
// (relative paths resolve against stateDir by the caller's use of it).
func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// OpenClawSoulPath resolves the SOUL.md destination among the configured
// OpenClaw workspaces: pick == "" requires exactly one workspace; otherwise
// pick is matched against a workspace ID, then treated as a directory when it
// looks like one (a path separator or a leading ~/.). home expands a
// leading "~" in pick — this package is injected-home throughout, so it
// never reads the real user home itself.
func OpenClawSoulPath(workspaces []OpenClawWorkspace, pick, home string) (string, error) {
	if pick == "" {
		switch len(workspaces) {
		case 1:
			return filepath.Join(workspaces[0].Path, "SOUL.md"), nil
		default:
			return "", output.ErrorWithHint(
				"several OpenClaw agents configured: "+DescribeOpenClawWorkspaces(workspaces),
				"re-run with --workspace <agent-id> (or --workspace <dir>)")
		}
	}

	for _, w := range workspaces {
		if w.ID == pick {
			return filepath.Join(w.Path, "SOUL.md"), nil
		}
	}

	if strings.ContainsRune(pick, filepath.Separator) ||
		strings.HasPrefix(pick, "~") || strings.HasPrefix(pick, ".") {
		return filepath.Join(expandHome(pick, home), "SOUL.md"), nil
	}

	ids := make([]string, len(workspaces))
	for i, w := range workspaces {
		ids[i] = w.ID
	}
	return "", output.ErrorWithHint(
		fmt.Sprintf("unknown OpenClaw agent %q (configured: %s)", pick, strings.Join(ids, ", ")),
		"re-run with --workspace <agent-id> (or --workspace <dir>)")
}

// DescribeOpenClawWorkspaces renders "<id> (<path>), <id> (<path>)..." —
// shared by every "several OpenClaw agents configured" error message
// (ResolveOpenClawWorkspaces callers included) so the format lives in one
// place.
func DescribeOpenClawWorkspaces(workspaces []OpenClawWorkspace) string {
	parts := make([]string, len(workspaces))
	for i, w := range workspaces {
		parts[i] = fmt.Sprintf("%s (%s)", w.ID, w.Path)
	}
	return strings.Join(parts, ", ")
}
