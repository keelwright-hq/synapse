package cli

import (
	"github.com/spf13/cobra"
	mcpserver "github.com/taricsa/synapse/internal/mcp"
	"github.com/taricsa/synapse/internal/store/badger"
)

var mcpRoot string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve Synapse context over MCP stdio",
	Long: `mcp starts a Model Context Protocol server on stdin/stdout.

Configure Cursor/Claude to launch: synapse mcp --data-dir .synapse --root .
Requires a prior synapse index of the repository.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := mcpRoot
		if root == "" {
			root = "."
		}
		repo, err := resolveRepoName(root)
		if err != nil {
			return err
		}
		store, err := badger.OpenWithRepo(dataDir, repo)
		if err != nil {
			return err
		}
		defer store.Close()

		s := mcpserver.New(mcpserver.Options{Store: store, RootDir: root})
		return mcpserver.ServeStdio(s)
	},
}

func init() {
	mcpCmd.Flags().StringVar(&mcpRoot, "root", ".", "Repository root for snippet extraction")
}
