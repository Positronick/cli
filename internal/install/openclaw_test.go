package install

import (
	"io/fs"
	"strings"
	"testing"
)

// envMap builds a getenv func over a fixed map — Getenv semantics: an unset
// key returns "".
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// readFileMap builds a readFile func over a fixed map of path -> content;
// any other path reports os.ErrNotExist via fs.PathError, like os.ReadFile.
func readFileMap(m map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if body, ok := m[path]; ok {
			return []byte(body), nil
		}
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
}

// TestResolveOpenClawWorkspaces covers the state-dir/config/workspace
// derivation rules verified against docs.openclaw.ai and a real install
// (see the PR-1 spec problem statement).
func TestResolveOpenClawWorkspaces(t *testing.T) {
	const home = "/home/ada"

	tests := []struct {
		name    string
		env     map[string]string
		files   map[string]string
		want    []OpenClawWorkspace
		wantErr string
	}{
		{
			name: "no config file: single main agent at <stateDir>/workspace",
			want: []OpenClawWorkspace{{ID: "main", Path: "/home/ada/.openclaw/workspace"}},
		},
		{
			name:  "config without agents key: treated as single-agent default",
			files: map[string]string{"/home/ada/.openclaw/openclaw.json": `{}`},
			want:  []OpenClawWorkspace{{ID: "main", Path: "/home/ada/.openclaw/workspace"}},
		},
		{
			name: "single main with agents.defaults.workspace",
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{"defaults":{"workspace":"/srv/agent-home"}}}`,
			},
			want: []OpenClawWorkspace{{ID: "main", Path: "/srv/agent-home"}},
		},
		{
			name: "OPENCLAW_WORKSPACE_DIR overrides the default agent's workspace",
			env:  map[string]string{"OPENCLAW_WORKSPACE_DIR": "/mnt/ws"},
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{"defaults":{"workspace":"/srv/agent-home"}}}`,
			},
			want: []OpenClawWorkspace{{ID: "main", Path: "/mnt/ws"}},
		},
		{
			name: "OPENCLAW_STATE_DIR relocates the state dir and default config path",
			env:  map[string]string{"OPENCLAW_STATE_DIR": "/opt/openclaw-state"},
			files: map[string]string{
				"/opt/openclaw-state/openclaw.json": `{}`,
			},
			want: []OpenClawWorkspace{{ID: "main", Path: "/opt/openclaw-state/workspace"}},
		},
		{
			name: "OPENCLAW_PROFILE picks ~/.openclaw-<profile> as the state dir",
			env:  map[string]string{"OPENCLAW_PROFILE": "staging"},
			files: map[string]string{
				"/home/ada/.openclaw-staging/openclaw.json": `{}`,
			},
			want: []OpenClawWorkspace{{ID: "main", Path: "/home/ada/.openclaw-staging/workspace"}},
		},
		{
			name: `OPENCLAW_PROFILE="default" does NOT change the state dir`,
			env:  map[string]string{"OPENCLAW_PROFILE": "default"},
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{}`,
			},
			want: []OpenClawWorkspace{{ID: "main", Path: "/home/ada/.openclaw/workspace"}},
		},
		{
			name: "multi-entry: explicit workspace used verbatim, ~ expanded",
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{"entries":{
					"main": {"default": true, "workspace": "~/work/main"},
					"researcher": {"workspace": "/data/research"}
				}}}`,
			},
			want: []OpenClawWorkspace{
				{ID: "main", Path: "/home/ada/work/main"},
				{ID: "researcher", Path: "/data/research"},
			},
		},
		{
			name: "multi-entry: default flagged entry wins over the entry named main",
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{"entries":{
					"main": {},
					"lead": {"default": true}
				}}}`,
			},
			want: []OpenClawWorkspace{
				{ID: "lead", Path: "/home/ada/.openclaw/workspace"},
				{ID: "main", Path: "/home/ada/.openclaw/workspace-main"},
			},
		},
		{
			name: "multi-entry: default-by-name main when no entry is flagged default",
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{"entries":{
					"main": {},
					"helper": {}
				}}}`,
			},
			want: []OpenClawWorkspace{
				{ID: "main", Path: "/home/ada/.openclaw/workspace"},
				{ID: "helper", Path: "/home/ada/.openclaw/workspace-helper"},
			},
		},
		{
			// docs.openclaw.ai: "setting a shared root does not by itself
			// bind it to any agent; pin agents.entries.main.workspace
			// explicitly to keep main on it" — agents.defaults.workspace is
			// a root for the NON-default entries only.
			name: "non-default entry without workspace derives <defaults.workspace>/<id>; the default agent ignores defaults.workspace",
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{
					"defaults":{"workspace":"/srv/agents"},
					"entries":{
						"main": {"default": true},
						"helper": {}
					}
				}}`,
			},
			want: []OpenClawWorkspace{
				{ID: "main", Path: "/home/ada/.openclaw/workspace"},
				{ID: "helper", Path: "/srv/agents/helper"},
			},
		},
		{
			name: "default agent without an explicit workspace uses OPENCLAW_WORKSPACE_DIR even when defaults.workspace is set",
			env:  map[string]string{"OPENCLAW_WORKSPACE_DIR": "/mnt/ws"},
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{
					"defaults":{"workspace":"/srv/agents"},
					"entries":{
						"main": {"default": true},
						"helper": {}
					}
				}}`,
			},
			want: []OpenClawWorkspace{
				{ID: "main", Path: "/mnt/ws"},
				{ID: "helper", Path: "/srv/agents/helper"},
			},
		},
		{
			name: "non-default entry without workspace or defaults.workspace derives <stateDir>/workspace-<id>",
			files: map[string]string{
				"/home/ada/.openclaw/openclaw.json": `{"agents":{"entries":{
					"main": {"default": true},
					"helper": {}
				}}}`,
			},
			want: []OpenClawWorkspace{
				{ID: "main", Path: "/home/ada/.openclaw/workspace"},
				{ID: "helper", Path: "/home/ada/.openclaw/workspace-helper"},
			},
		},
		{
			name:    "unparsable JSON is an error naming the file",
			files:   map[string]string{"/home/ada/.openclaw/openclaw.json": `{not json`},
			wantErr: "/home/ada/.openclaw/openclaw.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveOpenClawWorkspaces(home, envMap(tt.env), readFileMap(tt.files))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("error = nil, want one mentioning %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d workspaces, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("workspace[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestOpenClawSoulPath covers pick resolution: empty, several, by id, by
// dir, by ~-dir (expanded against the injected home, never os.UserHomeDir),
// unknown id.
func TestOpenClawSoulPath(t *testing.T) {
	const home = "/home/ada"
	single := []OpenClawWorkspace{{ID: "main", Path: "/home/ada/.openclaw/workspace"}}
	several := []OpenClawWorkspace{
		{ID: "main", Path: "/home/ada/.openclaw/workspace"},
		{ID: "researcher", Path: "/data/research"},
	}

	t.Run("single workspace, no pick", func(t *testing.T) {
		got, err := OpenClawSoulPath(single, "", home)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "/home/ada/.openclaw/workspace/SOUL.md"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("several workspaces, no pick errors listing every id and path", func(t *testing.T) {
		_, err := OpenClawSoulPath(several, "", home)
		if err == nil {
			t.Fatal("expected an error")
		}
		msg := err.Error()
		for _, want := range []string{
			"main (/home/ada/.openclaw/workspace)",
			"researcher (/data/research)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("error = %q, want it to contain %q", msg, want)
			}
		}
	})

	t.Run("pick by id", func(t *testing.T) {
		got, err := OpenClawSoulPath(several, "researcher", home)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "/data/research/SOUL.md"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("pick by dir", func(t *testing.T) {
		got, err := OpenClawSoulPath(several, "/tmp/custom-workspace", home)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "/tmp/custom-workspace/SOUL.md"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// The pick isn't anchored to the injected home used for detection by
	// accident of platform lookup — it must expand against the home this
	// function was given, so a test-injected home is honored deterministically.
	t.Run("pick by ~/dir expands with the injected home", func(t *testing.T) {
		got, err := OpenClawSoulPath(several, "~/custom-workspace", home)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "/home/ada/custom-workspace/SOUL.md"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("pick unknown id errors naming configured ids", func(t *testing.T) {
		_, err := OpenClawSoulPath(several, "ghost", home)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), `"ghost"`) ||
			!strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "researcher") {
			t.Errorf("error = %q, want the unknown id and configured ids named", err.Error())
		}
	})
}
