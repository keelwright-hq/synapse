package cli

import (
	"github.com/spf13/cobra"
	"github.com/keelwright-hq/synapse/internal/graph"
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
			wsPath := workspacePath
			dir := dataDir
			scope := repoName

			opened, err := openWorkspaceStore(wsPath, dir, scope)
			if err != nil {
				return err
			}
			var repoRoots map[string]string
			if opened.Workspace != nil {
				repoRoots = opened.Workspace.RepoRoots()
			}
			_ = opened.Closer.Close()

			opts := mcpserver.Options{
				RootDir:   root,
				RepoRoots: repoRoots,
				OpenSession: func() (graph.Store, []string, func(), error) {
					cur, err := openWorkspaceStore(wsPath, dir, scope)
					if err != nil {
						return nil, nil, nil, err
					}
					store := cur.Store
					if cur.Fed != nil {
						store = cur.Fed.Session()
					}
					return store, cur.Warnings, func() { _ = cur.Closer.Close() }, nil
				},
			}
			s := mcpserver.New(opts)
			return mcpserver.ServeStdio(s)
		}

		repo, err := resolveRepoName(root)
		if err != nil {
			return err
		}
		resolvedDataDir, err := resolveSingleRepoDataDir(cmd, root)
		if err != nil {
			return err
		}
		store, err := badger.OpenWithRepo(resolvedDataDir, repo)
		if err != nil {
			return err
		}
		_ = store.Close()

		s := mcpserver.New(mcpserver.Options{
			RootDir: root,
			OpenSession: func() (graph.Store, []string, func(), error) {
				cur, err := badger.OpenWithRepo(resolvedDataDir, repo)
				if err != nil {
					return nil, nil, nil, err
				}
				return cur, nil, func() { _ = cur.Close() }, nil
			},
		})
		return mcpserver.ServeStdio(s)
	},
}

func init() {
	mcpCmd.Flags().StringVar(&mcpRoot, "root", ".", "Repository root for snippet extraction (single-repo mode)")
}
