package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a repository into the local code graph (stub)",
	Long: `Index walks source files, parses ASTs via tree-sitter, and persists
nodes and edges into the embedded graph store.

This command is a scaffold stub; indexing lands in Phase 1 stories.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		fmt.Fprintf(cmd.OutOrStdout(), "index: not implemented yet (path=%s)\n", path)
	},
}
