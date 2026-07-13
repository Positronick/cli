package cli

import (
	"sort"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// This file owns the hidden admin `profile` command group (create + list)
// against /api/admin/profiles. Profiles were historically git-curated only;
// these are the admin self-service write/read paths. Like the other admin
// commands they are Hidden + annotated (revealed for cached admins) and map a
// 401/403 to exit 4 via adminAPIError. There is no create-by-file variant:
// a profile is a handful of flags, not a document.

// profileCreateResult is the `profile create --json` contract.
type profileCreateResult struct {
	Profile api.AdminProfile `json:"profile"`
	Created bool             `json:"created"`
}

// profileListResult is the `profile list --json` contract.
type profileListResult struct {
	Count    int                `json:"count"`
	Profiles []api.AdminProfile `json:"profiles"`
}

func newProfileCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "profile",
		Short: "Create and list registry profiles (admin)",
		Long: "Manage the authoring profiles that listings reference by handle. Profiles were " +
			"historically git-curated only; `profile create` is the admin self-service path " +
			"(source=api). verified/official default false — granting a seal stays explicit." +
			adminNote,
	})
	cmd.AddCommand(newProfileCreateCmd(), newProfileListCmd())
	return cmd
}

func newProfileCreateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "create --handle HANDLE --name NAME [--kind person|org]",
		Short: "Create a registry profile (admin)",
		Long: "Create an authoring profile so listings can be attributed to it. The id and source " +
			"are server-assigned (source=api, so the content seed never clobbers it). " +
			"--avatar-url defaults to the GitHub convention https://github.com/<handle>.png. " +
			"verified/official default false — a runtime-created author is not auto-sealed." +
			adminNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}

			fields := map[string]any{}
			// Required + optional string fields, flag→API-key. Empty optionals are
			// omitted so the server applies its defaults (e.g. the GitHub avatar) and
			// never sees an empty string where it expects a URL. An ordered slice (not
			// a map) keeps the wire body deterministic, like soulFieldFlagMap.
			if err := applyStringFieldFlags(cmd, [][2]string{
				{"handle", "handle"},
				{"name", "name"},
				{"kind", "kind"},
				{"avatar-url", "avatarUrl"},
				{"github-url", "githubUrl"},
				{"website", "website"},
				{"bio", "bio"},
			}, fields, true); err != nil {
				return err
			}
			// Bools are always sent (false is meaningful); granting a seal is opt-in.
			verified, gerr := cmd.Flags().GetBool("verified")
			if gerr != nil {
				return gerr
			}
			official, oerr := cmd.Flags().GetBool("official")
			if oerr != nil {
				return oerr
			}
			fields["verified"] = verified
			fields["official"] = official

			profile, err := client.CreateProfile(cmd.Context(), fields)
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(profileCreateResult{Profile: *profile, Created: true})
			}
			p.Human("Created profile @%s (%s, kind %s, id %s)\n",
				profile.Handle, profile.Name, profile.Kind, profile.ID)
			p.Human("Avatar: %s\n", deref(profile.AvatarURL))
			p.Human("Public path: /profiles/%s\n", profile.Handle)
			return nil
		},
	})
	f := cmd.Flags()
	f.String("handle", "", "profile handle (lowercase letters, digits, hyphens) — /profiles/<handle>")
	f.String("name", "", "display name")
	f.String("kind", "person", "profile kind: person or org")
	f.String("avatar-url", "", "avatar image URL (default: https://github.com/<handle>.png)")
	f.String("github-url", "", "GitHub profile URL")
	f.String("website", "", "website URL")
	f.String("bio", "", "short bio")
	f.Bool("verified", false, "grant the verified (white) seal")
	f.Bool("official", false, "grant the official (red) seal")
	for _, required := range []string{"handle", "name"} {
		_ = cmd.MarkFlagRequired(required)
	}
	return cmd
}

func newProfileListCmd() *cobra.Command {
	return markAdmin(&cobra.Command{
		Use:   "list",
		Short: "List all registry profiles (admin)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			profiles, err := client.Profiles(cmd.Context())
			if err != nil {
				return adminAPIError(err)
			}
			// Stable order for human + golden output; the API returns DB order.
			sort.Slice(profiles, func(i, j int) bool { return profiles[i].Handle < profiles[j].Handle })
			if p.Mode.JSON {
				return p.EmitJSON(profileListResult{Count: len(profiles), Profiles: profiles})
			}
			renderProfileTable(p, profiles)
			return nil
		},
	})
}

func renderProfileTable(p *output.Printer, profiles []api.AdminProfile) {
	rows := make([][]string, len(profiles))
	for i, pr := range profiles {
		rows[i] = []string{pr.Handle, pr.Name, pr.Kind, profileSeal(pr), pr.Source}
	}
	output.RenderTable(p.Out, []string{"HANDLE", "NAME", "KIND", "SEAL", "SOURCE"}, rows)
}

// profileSeal renders the trust tier: official (red) outranks verified (white).
func profileSeal(p api.AdminProfile) string {
	switch {
	case p.Official:
		return "official"
	case p.Verified:
		return "verified"
	default:
		return "—"
	}
}
