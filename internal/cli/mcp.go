package cli

import (
	"github.com/spf13/cobra"
	mcpserver "github.com/keelwright-hq/synapse/internal/mcp"
	"github.com/keelwright-hq/synapse/internal/store/badger"
)

var mcpRoot string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve Synapse context over MCP stdio",
	Long: `mcp starts a Model Context Protocol server on stdin/stdout.

Configure Cursor/Claude to launch:
  synapse mcp --data-dir .synapse --root .
  synapse mcp --workspace . --data-dir .synapse

Requires a prior synapse index (single-repo or --workspace).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := mcpRoot
		if root == "" {
			root = "."
		}

		if workspacePath != "" {
			opened, err := openWorkspaceStore(workspacePath, dataDir, repoName)
			if err != nil {
				return err
			}
			defer opened.Closer.Close()

			opts := mcpserver.Options{
				Store:        opened.Store,
				Federated:    opened.Fed,
				RootDir:      root,
				OpenWarnings: opened.Warnings,
			}
			if opened.Workspace != nil {
				opts.RepoRoots = opened.Workspace.RepoRoots()
			}
			s := mcpserver.New(opts)
			return mcpserver.ServeStdio(s)
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
	mcpCmd.Flags().StringVar(&mcpRoot, "root", ".", "Repository root for snippet extraction (single-repo mode)")
}
