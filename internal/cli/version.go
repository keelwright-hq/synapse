package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/keelwright-hq/synapse/internal/buildinfo"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print Synapse version and build metadata",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "synapse %s\ncommit: %s\nbuilt:  %s\n",
			buildinfo.Version, buildinfo.Commit, buildinfo.Date)
	},
}
