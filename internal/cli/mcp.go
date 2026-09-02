package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server over stdio (stub)",
	Long: `mcp starts a Model Context Protocol server on stdin/stdout so AI IDEs
(Cursor, Claude) can fetch live project context from Synapse.

This command is a scaffold stub; MCP integration lands in Phase 1 stories.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "mcp: not implemented yet")
	},
}
