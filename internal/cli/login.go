package cli

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/auth"
	"github.com/positronick/cli/internal/config"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// loginPending is the first line of the `login --json` NDJSON stream.
type loginPending struct {
	Status                  string `json:"status"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
}

// loginAuthenticated is the second line of the `login --json` NDJSON stream.
type loginAuthenticated struct {
	Status  string   `json:"status"`
	User    api.User `json:"user"`
	IsAdmin bool     `json:"isAdmin"`
}

// logoutResult is the `logout --json` contract.
type logoutResult struct {
	Status string `json:"status"`
}

const loginLong = `Authenticate this machine against positronick.com using the device flow.

The command prints a short code and a verification URL. Approve the code in
any browser — it does not have to be on this machine — and the CLI stores the
granted session token in <config dir>/credentials.json (mode 0600), keyed to
the base URL that issued it.

With --json the command streams NDJSON: exactly two lines, each a single JSON
object. The first is emitted immediately so callers can surface the code:

  {"status":"pending","userCode":"XXXX-XXXX","verificationUri":"...","verificationUriComplete":"...","expiresIn":1800}

and the second after the user approves:

  {"status":"authenticated","user":{"id":"...","name":"...","email":"...","image":"...","githubLogin":"..."},"isAdmin":false}

If the user denies the request the exit code is 2; if the code expires before
approval it is 4.`

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to positronick.com via the device flow",
		Long:  loginLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			noBrowser, err := cmd.Flags().GetBool("no-browser")
			if err != nil {
				return err
			}
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			baseURL, err := baseURLFor(cmd)
			if err != nil {
				return err
			}

			da, err := auth.Start(cmd.Context(), baseURL)
			if err != nil {
				return err
			}
			userCode := formatUserCode(da.UserCode)
			if p.Mode.JSON {
				err = p.EmitJSONLine(loginPending{
					Status:                  "pending",
					UserCode:                userCode,
					VerificationURI:         da.VerificationURI,
					VerificationURIComplete: da.VerificationURIComplete,
					ExpiresIn:               int(da.ExpiresIn.Seconds()),
				})
				if err != nil {
					return err
				}
			} else {
				p.Human("First, copy this code: %s\n", userCode)
				p.Human("Then visit: %s\n", da.VerificationURI)
			}
			if shouldOpenBrowser(p.Mode, noBrowser, runtime.GOOS,
				os.Getenv("DISPLAY"), os.Getenv("WAYLAND_DISPLAY")) {
				openBrowser(runtime.GOOS, da.VerificationURIComplete)
			}

			waiting := false
			tok, err := auth.Poll(cmd.Context(), baseURL, da.DeviceCode, da.Interval, da.ExpiresIn,
				func() {
					if !waiting {
						p.Status("Waiting for approval…\n")
						waiting = true
					}
				})
			if err != nil {
				return err
			}

			// Verify the session and capture the identity before persisting.
			client, err := api.New(baseURL, auth.TokenCredentials{Token: tok.AccessToken})
			if err != nil {
				return err
			}
			me, err := client.Me(cmd.Context())
			if err != nil {
				return err
			}
			dir, err := config.Dir()
			if err != nil {
				return err
			}
			nowUTC := time.Now().UTC()
			err = auth.Save(dir, &auth.Credentials{
				Version:     1,
				BaseURL:     baseURL,
				AccessToken: tok.AccessToken,
				TokenType:   tok.TokenType,
				ExpiresAt:   nowUTC.Add(tok.ExpiresIn).Format(time.RFC3339),
				User: auth.StoredUser{
					ID:      me.User.ID,
					Name:    me.User.Name,
					Email:   me.User.Email,
					IsAdmin: me.IsAdmin,
				},
				CreatedAt: nowUTC.Format(time.RFC3339),
			})
			if err != nil {
				return err
			}

			if p.Mode.JSON {
				return p.EmitJSONLine(loginAuthenticated{
					Status: "authenticated", User: me.User, IsAdmin: me.IsAdmin,
				})
			}
			p.Human("Logged in as %s (%s)\n", me.User.Name, me.User.Email)
			return nil
		},
	}
	cmd.Flags().Bool("no-browser", false, "never auto-open the verification page in a browser")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out — delete the cached credentials",
		Long: "Delete the credentials cached by `positronick login` for the current config " +
			"directory. Logging out with nothing cached is fine. POSITRONICK_API_KEY is an " +
			"environment variable, not a cached credential, so logout never touches it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := printerFor(cmd)
			if err != nil {
				return err
			}
			dir, err := config.Dir()
			if err != nil {
				return err
			}
			if err := auth.Delete(dir); err != nil {
				return err
			}
			if p.Mode.JSON {
				return p.EmitJSON(logoutResult{Status: "logged_out"})
			}
			p.Human("Logged out.\n")
			return nil
		},
	}
}

// formatUserCode renders the server's dash-less 8-character user code as
// XXXX-XXXX — easier to read and retype; the verification page accepts both
// forms. Anything unexpected passes through verbatim.
func formatUserCode(code string) string {
	if len(code) != 8 || strings.Contains(code, "-") {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// shouldOpenBrowser gates the login auto-open: only for a real person in an
// interactive session (TTY, not CI, not a coding agent), only where a
// browser can exist (macOS, or Linux with an X11/Wayland display), and never
// with --no-browser. Everyone else gets the printed code + URL.
func shouldOpenBrowser(m output.Mode, noBrowser bool, goos, display, waylandDisplay string) bool {
	if noBrowser || !m.Interactive() {
		return false
	}
	return goos == "darwin" || display != "" || waylandDisplay != ""
}

// openBrowser launches the platform URL opener, detached. Failures are
// deliberately ignored: the printed code + URL path always works.
var openBrowser = func(goos, url string) {
	opener := "xdg-open"
	if goos == "darwin" {
		opener = "open"
	}
	_ = exec.Command(opener, url).Start()
}
