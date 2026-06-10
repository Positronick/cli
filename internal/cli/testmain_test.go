package cli

import (
	"os"
	"testing"
)

// TestMain isolates the whole package from the developer's real environment:
// NewRootCmd now reads the credential cache at construction time (reveal-on-
// admin), so a developer logged in as an admin — or with a base-URL override
// exported — would otherwise flip hidden commands visible and break the
// agent-docs goldens. Individual tests still re-point the config dir with
// t.Setenv where they need their own cache.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "positronick-cli-test-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("POSITRONICK_CONFIG_DIR", dir)
	_ = os.Setenv("POSITRONICK_API_KEY", "")
	_ = os.Setenv("POSITRONICK_BASE_URL", "")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
