package mcpserver

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/mockapi"
)

// safeInstallPath guards soul_install's caller-supplied paths: an MCP server
// has no TTY to confirm a surprising write, so a relative path may not
// resolve outside the user's home directory. An absolute path is explicit
// intent and passes through as given.
func TestSafeInstallPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "ada")
	cwdInHome := filepath.Join(home, "project")
	cwdOutside := filepath.Join(string(filepath.Separator), "tmp", "work")

	tests := []struct {
		name    string
		path    string
		cwd     string
		want    string
		wantErr bool
	}{
		{
			"absolute path is honored as given",
			filepath.Join(string(filepath.Separator), "anywhere", "SOUL.md"),
			cwdOutside,
			filepath.Join(string(filepath.Separator), "anywhere", "SOUL.md"),
			false,
		},
		{
			"relative path under home resolves against cwd",
			filepath.Join("notes", "SOUL.md"),
			cwdInHome,
			filepath.Join(cwdInHome, "notes", "SOUL.md"),
			false,
		},
		{
			"dot-dot staying inside home resolves",
			filepath.Join("..", "other", "SOUL.md"),
			cwdInHome,
			filepath.Join(home, "other", "SOUL.md"),
			false,
		},
		{
			"dot-dot escaping home is refused",
			filepath.Join("..", "..", "..", "etc", "SOUL.md"),
			cwdInHome,
			"",
			true,
		},
		{
			"relative path outside home is refused",
			"SOUL.md",
			cwdOutside,
			"",
			true,
		},
		{
			"home itself is not a valid escape prefix trick",
			filepath.Join("..", "ada-evil", "SOUL.md"),
			home, // resolves to /home/ada-evil — sibling, not inside home
			"",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeInstallPath(tt.path, tt.cwd, home)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("safeInstallPath(%q) = %q, want an escape error", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeInstallPath(%q) error: %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("safeInstallPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// clearOpenClawEnv keeps a leaked OPENCLAW_* variable in the developer's own
// shell from changing where these tests expect the resolver to look.
func clearOpenClawEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"OPENCLAW_STATE_DIR", "OPENCLAW_PROFILE", "OPENCLAW_WORKSPACE_DIR"} {
		t.Setenv(k, "")
	}
}

// soul_install for target "openclaw" must resolve the real OpenClaw agent
// workspace (not install.TargetPath's ~/.openclaw): with no config, that is
// the no-config default workspace.
func TestMCPSoulInstallOpenClawSingleAgent(t *testing.T) {
	clearOpenClawEnv(t)
	cwd, home := t.TempDir(), t.TempDir()
	srv := httptest.NewServer(mockapi.InstallHandler())
	t.Cleanup(srv.Close)
	client, err := api.New(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Client: client, Cwd: cwd, Home: home}

	res, out, err := soulInstallHandler(opts)(context.Background(), nil,
		soulInstallIn{Slug: "sherlock", Target: "openclaw"})
	if err != nil {
		t.Fatalf("unexpected error: %v (result: %+v)", err, res)
	}
	wantPath := filepath.Join(home, ".openclaw", "workspace", "SOUL.md")
	if out.Installed.Path != wantPath {
		t.Errorf("installed path = %q, want %q", out.Installed.Path, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("SOUL.md missing at %s: %v", wantPath, err)
	}
}

// With several OpenClaw agents configured, the tool has no --workspace
// equivalent to pick one for the caller: it must fail loudly, naming every
// configured agent, and write nothing.
func TestMCPSoulInstallOpenClawSeveralAgentsErrors(t *testing.T) {
	clearOpenClawEnv(t)
	cwd, home := t.TempDir(), t.TempDir()
	mainWS := filepath.Join(home, "agents", "main-ws")
	researchWS := filepath.Join(home, "agents", "research-ws")
	dir := filepath.Join(home, ".openclaw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"agents":{"entries":{
		"main": {"default": true, "workspace": "` + filepath.ToSlash(mainWS) + `"},
		"researcher": {"workspace": "` + filepath.ToSlash(researchWS) + `"}
	}}}`
	if err := os.WriteFile(filepath.Join(dir, "openclaw.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mockapi.InstallHandler())
	t.Cleanup(srv.Close)
	client, err := api.New(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Client: client, Cwd: cwd, Home: home}

	_, _, err = soulInstallHandler(opts)(context.Background(), nil,
		soulInstallIn{Slug: "sherlock", Target: "openclaw"})
	if err == nil {
		t.Fatal("expected an error for several configured OpenClaw agents")
	}
	msg := err.Error()
	for _, want := range []string{
		"main (" + mainWS + ")",
		"researcher (" + researchWS + ")",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want it to contain %q", msg, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(mainWS, "SOUL.md")); statErr == nil {
		t.Error("nothing should have been written for a refused install")
	}
	if _, statErr := os.Stat(filepath.Join(researchWS, "SOUL.md")); statErr == nil {
		t.Error("nothing should have been written for a refused install")
	}
}
