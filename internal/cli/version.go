package cli

import (
	"runtime"

	"github.com/positronick/cli/internal/output"
	"github.com/positronick/cli/internal/version"
	"github.com/spf13/cobra"
)

type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the positronick CLI version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			jsonFlag, err := cmd.Flags().GetBool("json")
			if err != nil {
				return err
			}
			noColor, err := cmd.Flags().GetBool("no-color")
			if err != nil {
				return err
			}

			mode := output.DetectMode(jsonFlag, noColor)
			p := &output.Printer{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), Mode: mode}

			if mode.JSON {
				return p.EmitJSON(versionInfo{
					Version: version.Version,
					Commit:  version.Commit,
					Date:    version.Date,
					Go:      runtime.Version(),
				})
			}
			p.Human("%s\n", version.String())
			return nil
		},
	}
}
