package cli

import (
	"github.com/positronick/cli/internal/api"
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

// clientFor builds the API client from --base-url (resolved through env and
// config file by internal/config) with environment credentials.
func clientFor(cmd *cobra.Command) (*api.Client, error) {
	flagValue, err := cmd.Flags().GetString("base-url")
	if err != nil {
		return nil, err
	}
	baseURL, err := config.ResolveBaseURL(flagValue)
	if err != nil {
		return nil, err
	}
	return api.New(baseURL, api.EnvCredentials{})
}
