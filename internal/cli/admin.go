package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/auth"
	"github.com/positronick/cli/internal/config"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// This file owns the hidden admin write commands (soul/listing create +
// update and the loop-create sugar) against the /api/admin surface. They are
// Hidden and annotated; registerAdminCommands reveals them when the cached
// login says the user is an admin. Non-admins can still invoke them — the
// server answers 401/403 and adminAPIError maps that to exit 4 with a hint.

// adminAnnotation marks a command as admin-only so the reveal walk (and
// nothing else) can find it.
const adminAnnotation = "positronick.admin"

// adminNote is the shared tail of every admin command's Long help.
const adminNote = "\n\nRequires an admin account (positronick login). Ids are server-assigned " +
	"ULIDs — never supply one. There is no delete verb: unpublish with --status draft."

// loginAsAdminHint is the remediation attached to a 401/403 from /api/admin.
const loginAsAdminHint = "run positronick login with an admin account"

// defaultLoopSourceURL is the sourceUrl `loop create` uses unless overridden —
// the registry page first-party loops point at.
const defaultLoopSourceURL = "https://positronick.com/registry/loop"

// registerAdminCommands attaches the hidden admin commands to the existing
// tree by lookup — soul create/update under the soul noun, loop create under
// the loop noun, plus a hidden `listing` parent for the type-agnostic
// create/update — then reveals them all if the cached login is an admin.
func registerAdminCommands(root *cobra.Command) {
	if soul := findCommand(root, "soul"); soul != nil {
		soul.AddCommand(newSoulCreateCmd(), newSoulUpdateCmd())
	}
	if loop := findCommand(root, "loop"); loop != nil {
		loop.AddCommand(newLoopCreateCmd())
	}
	root.AddCommand(newListingCmd())
	root.AddCommand(newProfileCmd())
	root.AddCommand(newFeedCmd())
	revealAdminCommands(root)
}

// findCommand returns root's direct child with the given name, or nil.
func findCommand(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// markAdmin hides cmd and annotates it for the reveal walk.
func markAdmin(cmd *cobra.Command) *cobra.Command {
	cmd.Hidden = true
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[adminAnnotation] = "true"
	return cmd
}

// revealAdminCommands unhides every admin-annotated command when the cached
// login says the user is an admin, so help/completion/agent-docs reflect what
// the current user can actually do. Purely cosmetic and purely local — no
// network call; authorization is always the server's decision per request.
func revealAdminCommands(root *cobra.Command) {
	if !cachedIsAdmin() {
		return
	}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Annotations[adminAnnotation] == "true" {
			c.Hidden = false
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
}

// cachedIsAdmin reads the credential cache for the default-resolved base URL
// (no --base-url: flags are not parsed at construction time). Errors are
// deliberately swallowed — reveal is cosmetic, and a corrupt cache or config
// must not brick the CLI: `positronick logout`, the documented fix, has to
// keep working, and any real problem still surfaces on the first
// authenticated call.
func cachedIsAdmin() bool {
	dir, err := config.Dir()
	if err != nil {
		return false
	}
	baseURL, err := config.ResolveBaseURL("")
	if err != nil {
		return false
	}
	creds, err := auth.Load(dir, baseURL)
	if err != nil || creds == nil {
		return false
	}
	return creds.User.IsAdmin
}

// adminAPIError maps an /api/admin error onto the CLI error contract:
// 401/403 → exit 4 with the admin-login hint, 404 → exit 3, anything else
// (409 conflict, 422 invalid_input) → exit 1 carrying the server's message
// VERBATIM — on 422 it is the seed validator's message, the single rule set.
func adminAPIError(err error) error {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case api.IsAuthError(err):
		return output.AuthError(apiErr.Message, loginAsAdminHint)
	case api.IsNotFound(err):
		return output.NotFoundError(apiErr.Message, "")
	default:
		return &output.CodedError{Code: output.ExitError, ErrCode: apiErr.Code, Message: apiErr.Message}
	}
}

// isULID reports whether s is shaped like a ULID: 26 Crockford base32
// characters (no I, L, O, U), case-insensitive. Used to decide whether an
// id-or-slug argument that 404'd as an id is worth retrying as a slug.
func isULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, r := range strings.ToUpper(s) {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			return false
		}
	}
	return true
}

// splitFrontmatter splits a SOUL.md file into its YAML frontmatter text and
// markdown body. CRLF line endings are tolerated. A file that does not open
// with a "---" fence is all body; one that opens a fence and never closes it
// is an error — silently posting broken frontmatter as content would corrupt
// the soul.
func splitFrontmatter(raw string) (yamlText, body string, err error) {
	if !strings.HasPrefix(raw, "---\n") && !strings.HasPrefix(raw, "---\r\n") {
		return "", raw, nil
	}
	rest := raw[strings.Index(raw, "\n")+1:]
	pos := 0
	for pos <= len(rest) {
		lineEnd := strings.IndexByte(rest[pos:], '\n')
		var line string
		if lineEnd == -1 {
			line = rest[pos:]
			lineEnd = len(rest)
		} else {
			line = rest[pos : pos+lineEnd]
			lineEnd += pos
		}
		if strings.TrimRight(line, "\r") == "---" {
			if lineEnd >= len(rest) {
				return rest[:pos], "", nil
			}
			return rest[:pos], rest[lineEnd+1:], nil
		}
		if lineEnd == len(rest) {
			break
		}
		pos = lineEnd + 1
	}
	return "", "", output.Errorf("unterminated frontmatter: the opening --- has no closing ---")
}

// parseSoulFile parses a SOUL.md into the admin-API field map: the
// frontmatter keys verbatim — minus id, createdAt and updatedAt, which are
// server-assigned on the API path — plus the body as "content". Unknown keys
// are passed through on purpose: the server's validator is the single
// authority and its 422 message is surfaced verbatim.
func parseSoulFile(path, raw string) (map[string]any, error) {
	yamlText, body, err := splitFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if yamlText != "" {
		if err := yaml.Unmarshal([]byte(yamlText), &fields); err != nil {
			return nil, output.Errorf("parsing frontmatter in %s: %v", path, err)
		}
	}
	for _, serverAssigned := range []string{"id", "createdAt", "updatedAt"} {
		delete(fields, serverAssigned)
	}
	fields["content"] = body
	return fields, nil
}

// soulFieldFlagMap maps the soul field-override flags to their API field
// names (the repeatable --framework is handled separately).
var soulFieldFlagMap = [][2]string{
	{"slug", "slug"},
	{"name", "name"},
	{"author-handle", "authorHandle"},
	{"tagline", "tagline"},
	{"category", "category"},
	{"version", "version"},
	{"license", "license"},
}

// addSoulFieldFlags registers the per-field override flags shared by soul
// create and soul update.
func addSoulFieldFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("slug", "", "url slug (overrides frontmatter)")
	f.String("name", "", "display name (overrides frontmatter)")
	f.String("author-handle", "", "author handle (overrides frontmatter)")
	f.String("tagline", "", "one-line tagline (overrides frontmatter)")
	f.String("category", "", "soul category (overrides frontmatter)")
	f.StringArray("framework", nil, "supported framework, repeatable (overrides frontmatter)")
	f.String("version", "", "semver version (overrides frontmatter)")
	f.String("license", "", "SPDX license id (overrides frontmatter)")
}

// applySoulFieldFlags copies every explicitly set field flag over fields —
// flags always beat frontmatter.
func applySoulFieldFlags(cmd *cobra.Command, fields map[string]any) error {
	for _, pair := range soulFieldFlagMap {
		if !cmd.Flags().Changed(pair[0]) {
			continue
		}
		v, err := cmd.Flags().GetString(pair[0])
		if err != nil {
			return err
		}
		fields[pair[1]] = v
	}
	if cmd.Flags().Changed("framework") {
		v, err := cmd.Flags().GetStringArray("framework")
		if err != nil {
			return err
		}
		fields["frameworks"] = v
	}
	return nil
}

// soulCreateResult is the `soul create --json` contract.
type soulCreateResult struct {
	Soul    api.AdminSoul `json:"soul"`
	Created bool          `json:"created"`
}

// soulUpdateResult is the `soul update --json` contract — the server's
// response shape, tookOwnership included so agents can react to the flip.
type soulUpdateResult struct {
	Soul          api.AdminSoul `json:"soul"`
	TookOwnership bool          `json:"tookOwnership"`
}

// listingCreateResult is the `listing create` / `loop create` --json contract.
type listingCreateResult struct {
	Listing api.AdminListing `json:"listing"`
	Created bool             `json:"created"`
}

// listingUpdateResult is the `listing update --json` contract.
type listingUpdateResult struct {
	Listing       api.AdminListing `json:"listing"`
	TookOwnership bool             `json:"tookOwnership"`
}

// ownershipWarning prints the seed→api flip warning. It rides Status (stderr,
// suppressed in JSON mode where the tookOwnership field says the same thing).
func ownershipWarning(p *output.Printer, took bool, what string) {
	if !took {
		return
	}
	p.Status("WARNING: took ownership from the seed — the git copy of this %s is now inert "+
		"and deploys will skip it (update with --source seed to hand it back).\n", what)
}

func newSoulCreateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "create --file SOUL.md",
		Short: "Create a soul on positronick.com (admin)",
		Long: "Create a soul directly in the registry from a SOUL.md file (YAML frontmatter + " +
			"markdown body, the same format as src/content/souls). Field flags override " +
			"frontmatter values. Any id, createdAt or updatedAt in the frontmatter is ignored — " +
			"the server assigns them — and the row is api-owned, so the content seed never " +
			"touches it. Validation is the seed's own validator; a 422 carries its message " +
			"verbatim." + adminNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			fields, err := soulFieldsFromFile(cmd)
			if err != nil {
				return err
			}
			if err := applySoulFieldFlags(cmd, fields); err != nil {
				return err
			}
			if err := applyStringFlag(cmd, "status", fields); err != nil {
				return err
			}

			soul, err := client.CreateSoul(cmd.Context(), fields)
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(soulCreateResult{Soul: *soul, Created: true})
			}
			p.Human("Created soul %s (id %s, status %s)\n", soul.Slug, soul.ID, soul.Status)
			p.Human("Public path: /souls/%s\n", soul.Slug)
			return nil
		},
	})
	cmd.Flags().String("file", "", "path to the SOUL.md file (YAML frontmatter + markdown body)")
	_ = cmd.MarkFlagRequired("file")
	addSoulFieldFlags(cmd)
	cmd.Flags().String("status", "", "publication status: draft, pending or published (default: published)")
	return cmd
}

func newSoulUpdateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "update <id-or-slug>",
		Short: "Update a soul on positronick.com (admin)",
		Long: "Patch a soul in place: only the fields you provide change. --file replaces the " +
			"frontmatter fields and the whole SOUL.md body; field flags override either way. " +
			"The argument is the soul id, or a published slug (drafts are only addressable by " +
			"id). Updating a seed-owned soul takes api ownership — its git copy goes inert and " +
			"deploys skip it; pass --source seed to hand ownership back to the content " +
			"pipeline." + adminNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			patch := map[string]any{}
			if cmd.Flags().Changed("file") {
				fileFields, err := soulFieldsFromFile(cmd)
				if err != nil {
					return err
				}
				for k, v := range fileFields {
					patch[k] = v
				}
			}
			if err := applySoulFieldFlags(cmd, patch); err != nil {
				return err
			}
			for _, flag := range []string{"status", "source"} {
				if err := applyStringFlag(cmd, flag, patch); err != nil {
					return err
				}
			}
			if len(patch) == 0 {
				return output.Errorf("nothing to update — provide --file, field flags, --status or --source")
			}

			ref := args[0]
			soul, took, err := client.UpdateSoul(cmd.Context(), ref, patch)
			if api.IsNotFound(err) && !isULID(ref) {
				// Not an id and not shaped like one: resolve it as a slug via
				// the public detail endpoint and retry.
				var id string
				if id, err = soulIDForSlug(cmd.Context(), client, ref); err != nil {
					return err
				}
				soul, took, err = client.UpdateSoul(cmd.Context(), id, patch)
			}
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(soulUpdateResult{Soul: *soul, TookOwnership: took})
			}
			p.Human("Updated soul %s (id %s, status %s)\n", soul.Slug, soul.ID, soul.Status)
			ownershipWarning(p, took, "soul")
			return nil
		},
	})
	cmd.Flags().String("file", "", "SOUL.md replacing the frontmatter fields and the whole body")
	addSoulFieldFlags(cmd)
	cmd.Flags().String("status", "", "publication status: draft (= unpublish), pending or published")
	cmd.Flags().String("source", "", "ownership: api (default on any change) or seed (hand back to git)")
	return cmd
}

// soulFieldsFromFile reads and parses the --file SOUL.md into the field map.
func soulFieldsFromFile(cmd *cobra.Command) (map[string]any, error) {
	path, err := cmd.Flags().GetString("file")
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, output.Errorf("reading %s: %v", path, err)
	}
	return parseSoulFile(path, string(raw))
}

// applyStringFlag copies a string flag into fields when explicitly set.
func applyStringFlag(cmd *cobra.Command, name string, fields map[string]any) error {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		return err
	}
	fields[name] = v
	return nil
}

// soulIDForSlug resolves a published slug to its soul id via the public
// detail endpoint.
func soulIDForSlug(ctx context.Context, client *api.Client, slug string) (string, error) {
	soul, err := client.Soul(ctx, slug)
	if api.IsNotFound(err) {
		return "", output.NotFoundError(fmt.Sprintf("soul %q not found", slug),
			"pass the soul id — drafts are only addressable by id. Run: positronick soul list")
	}
	if err != nil {
		return "", err
	}
	return soul.ID, nil
}

// listingIDForSlug resolves a published slug to its listing id via the public
// detail endpoint.
func listingIDForSlug(ctx context.Context, client *api.Client, slug string) (string, error) {
	listing, err := client.Listing(ctx, slug)
	if api.IsNotFound(err) {
		return "", output.NotFoundError(fmt.Sprintf("listing %q not found", slug),
			"pass the listing id — drafts are only addressable by id")
	}
	if err != nil {
		return "", err
	}
	return listing.ID, nil
}

// printerAndClient is the shared per-invocation setup for admin commands.
func printerAndClient(cmd *cobra.Command) (*output.Printer, *api.Client, error) {
	p, err := printerFor(cmd)
	if err != nil {
		return nil, nil, err
	}
	client, err := clientFor(cmd)
	if err != nil {
		return nil, nil, err
	}
	return p, client, nil
}

func newListingCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "listing",
		Short: "Create and update registry listings (admin)",
		Long: "Write access to registry listings of any type — the public type nouns (harness, " +
			"cli, mcp, agent, skill, plugin, loop) stay read-only. Listings are authored by an " +
			"existing profile handle; profiles themselves stay git-curated." + adminNote,
	})
	cmd.AddCommand(newListingCreateCmd(), newListingUpdateCmd())
	return cmd
}

// listingFieldsFromFile reads the --file JSON object into a field map,
// dropping the server-assigned id and the server-forced official flag so a
// listing copied verbatim from src/content/profiles just works.
func listingFieldsFromFile(cmd *cobra.Command) (map[string]any, error) {
	path, err := cmd.Flags().GetString("file")
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, output.Errorf("reading %s: %v", path, err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, output.Errorf("parsing %s: %v (the file must hold one JSON listing object)", path, err)
	}
	delete(fields, "id")       // server-assigned
	delete(fields, "official") // server-forced to true
	return fields, nil
}

func newListingCreateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "create --file listing.json",
		Short: "Create a registry listing of any type (admin)",
		Long: "Create a listing from a JSON file holding one listing object — the same shape as " +
			"the listings entries in src/content/profiles/*.json (any id or official flag in the " +
			"file is ignored; ids are server-assigned and official is always true). The " +
			"authoring profile is referenced by handle and must already exist. Loops must carry " +
			"data.goal, data.checkCommand and data.exitCondition — or use `positronick loop " +
			"create`, which builds them from flags." + adminNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			fields, err := listingFieldsFromFile(cmd)
			if err != nil {
				return err
			}
			if err := applyProfileFlag(cmd, fields); err != nil {
				return err
			}
			if err := applyStringFlag(cmd, "status", fields); err != nil {
				return err
			}
			return emitListingCreate(cmd.Context(), p, client, fields)
		},
	})
	cmd.Flags().String("file", "", "path to a JSON file holding one listing object")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().String("profile", "", "authoring profile handle (overrides profileHandle in the file)")
	cmd.Flags().String("status", "", "publication status: draft, pending or published (default: published)")
	return cmd
}

func newListingUpdateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "update <id-or-slug>",
		Short: "Update a registry listing of any type (admin)",
		Long: "Patch a listing in place: only the fields you provide change. --file is a JSON " +
			"object of fields to set; --profile re-assigns the authoring profile by handle. The " +
			"argument is the listing id, or a published slug (drafts are only addressable by " +
			"id). Updating a seed-owned listing takes api ownership — its git copy goes inert " +
			"and deploys skip it; pass --source seed to hand it back." + adminNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			patch := map[string]any{}
			if cmd.Flags().Changed("file") {
				fileFields, err := listingFieldsFromFile(cmd)
				if err != nil {
					return err
				}
				for k, v := range fileFields {
					patch[k] = v
				}
			}
			if err := applyProfileFlag(cmd, patch); err != nil {
				return err
			}
			for _, flag := range []string{"status", "source"} {
				if err := applyStringFlag(cmd, flag, patch); err != nil {
					return err
				}
			}
			if len(patch) == 0 {
				return output.Errorf("nothing to update — provide --file, --profile, --status or --source")
			}

			ref := args[0]
			listing, took, err := client.UpdateListing(cmd.Context(), ref, patch)
			if api.IsNotFound(err) && !isULID(ref) {
				var id string
				if id, err = listingIDForSlug(cmd.Context(), client, ref); err != nil {
					return err
				}
				listing, took, err = client.UpdateListing(cmd.Context(), id, patch)
			}
			if err != nil {
				return adminAPIError(err)
			}
			if p.Mode.JSON {
				return p.EmitJSON(listingUpdateResult{Listing: *listing, TookOwnership: took})
			}
			p.Human("Updated listing %s (type %s, id %s, status %s)\n",
				listing.Slug, listing.Type, listing.ID, listing.Status)
			ownershipWarning(p, took, "listing")
			return nil
		},
	})
	cmd.Flags().String("file", "", "JSON object of listing fields to set")
	cmd.Flags().String("profile", "", "re-assign the authoring profile by handle")
	cmd.Flags().String("status", "", "publication status: draft (= unpublish), pending or published")
	cmd.Flags().String("source", "", "ownership: api (default on any change) or seed (hand back to git)")
	return cmd
}

// applyProfileFlag copies --profile into the profileHandle field when set.
func applyProfileFlag(cmd *cobra.Command, fields map[string]any) error {
	if !cmd.Flags().Changed("profile") {
		return nil
	}
	v, err := cmd.Flags().GetString("profile")
	if err != nil {
		return err
	}
	fields["profileHandle"] = v
	return nil
}

// emitListingCreate posts the listing and renders the create result.
func emitListingCreate(ctx context.Context, p *output.Printer, client *api.Client, fields map[string]any) error {
	listing, err := client.CreateListing(ctx, fields)
	if err != nil {
		return adminAPIError(err)
	}
	if p.Mode.JSON {
		return p.EmitJSON(listingCreateResult{Listing: *listing, Created: true})
	}
	p.Human("Created listing %s (type %s, id %s, status %s)\n",
		listing.Slug, listing.Type, listing.ID, listing.Status)
	p.Human("Public path: /listings/%s\n", listing.Slug)
	return nil
}

func newLoopCreateCmd() *cobra.Command {
	cmd := markAdmin(&cobra.Command{
		Use:   "create --name NAME --slug SLUG --goal GOAL --check-command CMD --exit-condition COND --profile HANDLE",
		Short: "Create a loop listing from its run parameters (admin)",
		Long: "Sugar over `positronick listing create` for type=loop: builds the listing from " +
			"the loop's run parameters (goal, check command, exit condition) instead of a JSON " +
			"file. The tagline defaults to the goal and the category to AI/ML. The authoring " +
			"profile is referenced by handle and must already exist." + adminNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, client, err := printerAndClient(cmd)
			if err != nil {
				return err
			}
			str := func(name string) string {
				v, gerr := cmd.Flags().GetString(name)
				if gerr != nil && err == nil {
					err = gerr
				}
				return v
			}
			name, slug, goal := str("name"), str("slug"), str("goal")
			checkCommand, exitCondition := str("check-command"), str("exit-condition")
			profile, tagline, category, sourceURL := str("profile"), str("tagline"), str("category"), str("source-url")
			kickoff, kickoffFile := str("kickoff"), str("kickoff-file")
			maxIterations, gerr := cmd.Flags().GetInt("max-iterations")
			if gerr != nil && err == nil {
				err = gerr
			}
			if err != nil {
				return err
			}
			if kickoffFile != "" {
				raw, rerr := os.ReadFile(kickoffFile)
				if rerr != nil {
					return output.Errorf("reading %s: %v", kickoffFile, rerr)
				}
				kickoff = string(raw)
			}
			if tagline == "" {
				tagline = goal
			}

			data := map[string]any{
				"goal":          goal,
				"checkCommand":  checkCommand,
				"exitCondition": exitCondition,
			}
			if maxIterations > 0 {
				data["maxIterations"] = maxIterations
			}
			if kickoff != "" {
				data["kickoff"] = kickoff
			}
			fields := map[string]any{
				"slug":          slug,
				"name":          name,
				"type":          "loop",
				"tagline":       tagline,
				"category":      category,
				"sourceUrl":     sourceURL,
				"profileHandle": profile,
				"data":          data,
			}
			return emitListingCreate(cmd.Context(), p, client, fields)
		},
	})
	f := cmd.Flags()
	f.String("name", "", "display name of the loop")
	f.String("slug", "", "url slug of the loop")
	f.String("goal", "", "what \"done\" looks like for the loop")
	f.String("check-command", "", "command run between iterations to gauge progress")
	f.String("exit-condition", "", "condition that ends the loop")
	f.Int("max-iterations", 0, "safety cap on iterations (0 = none)")
	f.String("kickoff", "", "kickoff prompt a user copies to start the loop")
	f.String("kickoff-file", "", "file holding the kickoff prompt")
	f.String("profile", "", "authoring profile handle (must exist)")
	f.String("tagline", "", "one-line tagline (default: the goal)")
	f.String("category", "AI/ML", "listing category")
	f.String("source-url", defaultLoopSourceURL, "official source URL for the loop")
	for _, required := range []string{"name", "slug", "goal", "check-command", "exit-condition", "profile"} {
		_ = cmd.MarkFlagRequired(required)
	}
	cmd.MarkFlagsMutuallyExclusive("kickoff", "kickoff-file")
	return cmd
}
