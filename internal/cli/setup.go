package cli

import (
	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/auth"
	"github.com/positronick/cli/internal/config"
	"github.com/positronick/cli/internal/output"
	"github.com/spf13/cobra"
)

// printerFor builds the invocation's Printer from the persistent flags and
// the detected environment. It is the only place commands resolve output mode.
func printerFor(cmd *cobra.Command) (*output.Printer, error) {
	jsonFlag, err := cmd.Flags().GetBool("json")
	if err != nil {
		return nil, err
	}
	noColor, err := cmd.Flags().GetBool("no-color")
	if err != nil {
		return nil, err
	}
	quiet, err := cmd.Flags().GetBool("quiet")
	if err != nil {
		return nil, err
	}
	return &output.Printer{
		Out:   cmd.OutOrStdout(),
		Err:   cmd.ErrOrStderr(),
		Mode:  output.DetectMode(jsonFlag, noColor),
		Quiet: quiet,
	}, nil
}

// baseURLFor resolves the API base URL from --base-url through env, config
// file and default (see config.ResolveBaseURL).
func baseURLFor(cmd *cobra.Command) (string, error) {
	flagValue, err := cmd.Flags().GetString("base-url")
	if err != nil {
		return "", err
	}
	return config.ResolveBaseURL(flagValue)
}

// clientFor builds the API client for the resolved base URL with the full
// credential chain (auth.Provider): POSITRONICK_API_KEY, else the cached
// login for that base URL, else anonymous.
func clientFor(cmd *cobra.Command) (*api.Client, error) {
	baseURL, err := baseURLFor(cmd)
	if err != nil {
		return nil, err
	}
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return api.New(baseURL, auth.NewProvider(dir, baseURL))
}
