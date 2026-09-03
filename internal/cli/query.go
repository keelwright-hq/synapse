package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/taricsa/synapse/internal/rank"
	"github.com/taricsa/synapse/internal/store/badger"
)

var (
	queryDepth    int
	queryMaxNodes int
	queryBudget   int
	queryRoot     string
	queryJSON     bool
)

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query the local code graph",
	Long:  `query inspects the local code graph (neighborhood lookups, etc.).`,
}

var queryNeighborhoodCmd = &cobra.Command{
	Use:   "neighborhood <symbol>",
	Short: "Ranked neighborhood around a symbol id or name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if workspacePath != "" {
			return runWorkspaceNeighborhood(cmd, args[0])
		}

		repo, err := resolveRepoName(queryRoot)
		if err != nil {
			return err
		}
		store, err := badger.OpenWithRepo(dataDir, repo)
		if err != nil {
			return err
		}
		defer store.Close()

		seed, err := rank.ResolveSeed(store, args[0])
		if err != nil {
			return err
		}
		res, err := rank.Neighborhood(store, seed, rank.Options{
			Depth:    queryDepth,
			MaxNodes: queryMaxNodes,
			Budget:   queryBudget,
			RootDir:  queryRoot,
		})
		if err != nil {
			return err
		}
		return writeNeighborhood(cmd, res)
	},
}

func runWorkspaceNeighborhood(cmd *cobra.Command, symbol string) error {
	store, ws, c, err := openWorkspaceStore(workspacePath, dataDir, repoName)
	if err != nil {
		return err
	}
	defer c.Close()

	seed, err := rank.ResolveSeed(store, symbol)
	if err != nil {
		return err
	}
	opts := rank.Options{
		Depth:     queryDepth,
		MaxNodes:  queryMaxNodes,
		Budget:    queryBudget,
		RepoRoots: ws.RepoRoots(),
	}
	if repoName != "" {
		member, err := ws.Lookup(repoName)
		if err != nil {
			return err
		}
		opts.RootDir = member.Path
	}
	res, err := rank.Neighborhood(store, seed, opts)
	if err != nil {
		return err
	}
	return writeNeighborhood(cmd, res)
}

func writeNeighborhood(cmd *cobra.Command, res rank.Result) error {
	if queryJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "seed=%s hits=%d truncated=%v\n", res.Seed, len(res.Hits), res.Truncated)
	for _, h := range res.Hits {
		fmt.Fprintf(cmd.OutOrStdout(), "  [%.3f d=%d] %s %s %s (%s)\n",
			h.Score, h.Depth, h.Kind, h.ID, h.Path, h.EdgeReason)
		if h.Snippet != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", truncateOneLine(h.Snippet, 120))
		}
	}
	return nil
}

func truncateOneLine(s string, n int) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			s = s[:i] + "…"
			break
		}
	}
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func init() {
	queryNeighborhoodCmd.Flags().IntVar(&queryDepth, "depth", 2, "Neighborhood depth")
	queryNeighborhoodCmd.Flags().IntVar(&queryMaxNodes, "max-nodes", 32, "Maximum nodes to return")
	queryNeighborhoodCmd.Flags().IntVar(&queryBudget, "budget", 0, "Character budget (0 = unlimited)")
	queryNeighborhoodCmd.Flags().StringVar(&queryRoot, "root", ".", "Repo root for snippet extraction")
	queryNeighborhoodCmd.Flags().BoolVar(&queryJSON, "json", false, "Emit JSON")
	queryCmd.AddCommand(queryNeighborhoodCmd)

	// Keep a helpful default when `synapse query` is invoked alone.
	queryCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	}
}
