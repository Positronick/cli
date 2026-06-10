package auth

import (
	"fmt"
	"os"

	"github.com/positronick/cli/internal/api"
)

// Provider is the CLI's api.CredentialsProvider. Precedence per request:
// POSITRONICK_API_KEY from the environment (sent as x-api-key, never
// persisted) wins; otherwise the cached bearer for the resolved base URL;
// otherwise anonymous. Both sources are read at call time, so a key exported
// or a login completed mid-process is picked up.
type Provider struct {
	dir     string
	baseURL string
}

// NewProvider returns a Provider reading the credential cache in dir, keyed
// to baseURL.
func NewProvider(dir, baseURL string) *Provider {
	return &Provider{dir: dir, baseURL: baseURL}
}

// Get implements api.CredentialsProvider.
func (p *Provider) Get() (api.Credentials, error) {
	if key := os.Getenv("POSITRONICK_API_KEY"); key != "" {
		return api.Credentials{APIKey: key}, nil
	}
	stored, err := Load(p.dir, p.baseURL)
	if err != nil {
		return api.Credentials{}, fmt.Errorf("loading cached credentials: %w", err)
	}
	if stored != nil {
		return api.Credentials{Bearer: stored.AccessToken}, nil
	}
	return api.Credentials{}, nil
}

// TokenCredentials is an api.CredentialsProvider pinned to one bearer token.
// It is used where the session token must be presented even when an env API
// key exists — e.g. minting API keys, which the server restricts to sessions.
type TokenCredentials struct {
	Token string
}

// Get implements api.CredentialsProvider.
func (t TokenCredentials) Get() (api.Credentials, error) {
	return api.Credentials{Bearer: t.Token}, nil
}
