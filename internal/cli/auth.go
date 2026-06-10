package cli

import (
	"os"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/auth"
	"github.com/positronick/cli/internal/config"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// authStatusResult is the `auth status --json` contract. Method is "api_key"
// or "device", null when no credentials resolve; user is omitted unless the
// live check succeeded; hint appears when credentials were rejected.
type authStatusResult struct {
	Authenticated bool      `json:"authenticated"`
	Method        *string   `json:"method"`
	User          *api.User `json:"user,omitempty"`
	IsAdmin       bool      `json:"isAdmin"`
	BaseURL       string    `json:"baseUrl"`
	Hint          string    `json:"hint,omitempty"`
}

// apiKeyResult is the `auth token create --json` contract.
type apiKeyResult struct {
	APIKey api.APIKey `json:"apiKey"`
}

// maxKeyExpiryDays is the client-side cap on --expires-days; the server
// validates the same limit.
const maxKeyExpiryDays = 365

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect authentication and manage API keys",
	}
	cmd.AddCommand(newAuthStatusCmd(), newAuthTokenCmd())
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who you are authenticated as (live check)",
		Long: "Resolve credentials the way every command does — POSITRONICK_API_KEY first, " +
			"then the cached login for the resolved base URL — and verify them live against " +
			"/api/me. Status is a query, not an assertion: it exits 0 whether or not you are " +
			"authenticated (including when stored credentials are rejected); only an unreachable " +
			"server is an error. A successful check refreshes the cached identity.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			baseURL, err := baseURLFor(cmd)
			if err != nil {
				return err
			}
			dir, err := config.Dir()
			if err != nil {
				return err
			}

			// Resolve credentials exactly like auth.Provider, but remember
			// which source won so the method can be reported.
			var method string
			var creds api.CredentialsProvider
			var stored *auth.Credentials
			if key := os.Getenv("POSITRONICK_API_KEY"); key != "" {
				method, creds = "api_key", api.EnvCredentials{}
			} else {
				if stored, err = auth.Load(dir, baseURL); err != nil {
					return err
				}
				if stored != nil {
					method, creds = "device", auth.TokenCredentials{Token: stored.AccessToken}
				}
			}

			if method == "" {
				return renderAuthStatus(p, authStatusResult{
					Method:  nil,
					BaseURL: baseURL,
					Hint:    "run positronick login",
				})
			}

			client, err := api.New(baseURL, creds)
			if err != nil {
				return err
			}
			me, err := client.Me(cmd.Context())
			if api.IsAuthError(err) {
				return renderAuthStatus(p, authStatusResult{
					Method:  &method,
					BaseURL: baseURL,
					Hint:    "credentials were rejected — run positronick login",
				})
			}
			if err != nil {
				return err
			}

			// Refresh the cached identity and isAdmin bit on a live success.
			if stored != nil {
				stored.User = auth.StoredUser{
					ID:      me.User.ID,
					Name:    me.User.Name,
					Email:   me.User.Email,
					IsAdmin: me.IsAdmin,
				}
				if err := auth.Save(dir, stored); err != nil {
					return err
				}
			}

			return renderAuthStatus(p, authStatusResult{
				Authenticated: true,
				Method:        &method,
				User:          &me.User,
				IsAdmin:       me.IsAdmin,
				BaseURL:       baseURL,
			})
		},
	}
}

// renderAuthStatus emits one auth-status result: the JSON contract, or the
// human field view with the hint (if any) as a stderr status line.
func renderAuthStatus(p *output.Printer, res authStatusResult) error {
	if p.Mode.JSON {
		return p.EmitJSON(res)
	}
	method, user, email, admin := "none", "", "", ""
	if res.Method != nil {
		method = *res.Method
	}
	if res.User != nil {
		user, email = res.User.Name, res.User.Email
	}
	if res.Authenticated {
		admin = yesNo(res.IsAdmin)
	}
	output.RenderFields(p.Out, fieldRows(
		"AUTHENTICATED", yesNo(res.Authenticated),
		"METHOD", method,
		"USER", user,
		"EMAIL", email,
		"ADMIN", admin,
		"BASE URL", res.BaseURL,
	))
	if res.Hint != "" {
		p.Status("hint: %s\n", res.Hint)
	}
	return nil
}

func newAuthTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API keys for unattended use",
	}
	cmd.AddCommand(newAuthTokenCreateCmd())
	return cmd
}

func newAuthTokenCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an API key (shown exactly once)",
		Long: "Mint a new API key for unattended use (CI, servers, agents), to be passed as " +
			"the POSITRONICK_API_KEY environment variable. Requires a logged-in session: the " +
			"server does not allow an API key to create more API keys. The raw key is shown " +
			"exactly once — the server stores only a hash. Without --expires-days the server " +
			"applies its default expiry (90 days).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := cmd.Flags().GetString("name")
			if err != nil {
				return err
			}
			days, err := cmd.Flags().GetInt("expires-days")
			if err != nil {
				return err
			}
			if days != 0 && (days < 1 || days > maxKeyExpiryDays) {
				return output.Errorf("--expires-days must be between 1 and %d, got %d",
					maxKeyExpiryDays, days)
			}
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			baseURL, err := baseURLFor(cmd)
			if err != nil {
				return err
			}
			dir, err := config.Dir()
			if err != nil {
				return err
			}

			stored, err := auth.Load(dir, baseURL)
			if err != nil {
				return err
			}
			if stored == nil {
				if os.Getenv("POSITRONICK_API_KEY") != "" {
					return output.AuthError("an API key cannot create API keys",
						"log in first: run positronick login")
				}
				return output.AuthError("not logged in", "run positronick login first")
			}

			client, err := api.New(baseURL, auth.TokenCredentials{Token: stored.AccessToken})
			if err != nil {
				return err
			}
			key, err := client.CreateAPIKey(cmd.Context(), name, days*24*60*60)
			if api.IsAuthError(err) {
				return output.AuthError("session expired", "run positronick login again")
			}
			if err != nil {
				return err
			}

			if p.Mode.JSON {
				return p.EmitJSON(apiKeyResult{APIKey: *key})
			}
			p.Human("%s\n", key.Key)
			p.Status("Store it now — it will never be shown again.\n")
			p.Status("Use it as: export POSITRONICK_API_KEY=%s\n", key.Key)
			return nil
		},
	}
	cmd.Flags().String("name", "positronick-cli", "name for the new key")
	cmd.Flags().Int("expires-days", 0, "key lifetime in days, 1-365 (0 = server default, 90)")
	return cmd
}

// yesNo renders a boolean for the human field view.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
