package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query the code graph (stub)",
	Long: `query inspects the local code graph for debugging (e.g. neighborhood
lookups around a symbol).

This command is a scaffold stub; query subcommands land in Phase 1 stories.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "query: not implemented yet")
	},
}
