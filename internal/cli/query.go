package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/badger"
	"github.com/spf13/cobra"
)

var (
	queryDepth    int
	queryMaxNodes int
	queryBudget   int
	queryRoot     string
	queryJSON     bool
	queryExplain  bool
	queryLimit    int
)

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query the local code graph",
	Long:  `query inspects the local code graph (neighborhood lookups, semantic Phase 3 queries, etc.).`,
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
			Explain:  queryExplain,
		})
		if err != nil {
			return err
		}
		return writeNeighborhood(cmd, res)
	},
}

var queryCoChangesCmd = &cobra.Command{
	Use:   "co-changes <path>",
	Short: "Top git co-change neighbors for a file path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, cleanup, err := openMemberQueryStore()
		if err != nil {
			return err
		}
		defer cleanup()
		hits, err := rank.CoChanges(store, args[0], queryLimit)
		if err != nil {
			return err
		}
		if queryJSON {
			return writeJSON(cmd, hits)
		}
		for _, h := range hits {
			fmt.Fprintf(cmd.OutOrStdout(), "  [%.3f support=%d] %s %s\n", h.Weight, h.Support, h.ID, h.Path)
		}
		return nil
	},
}

var queryHotPathsCmd = &cobra.Command{
	Use:   "hot-paths <symbol>",
	Short: "Runtime observed_calls hot paths for a symbol",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, cleanup, err := openMemberQueryStore()
		if err != nil {
			return err
		}
		defer cleanup()
		hits, err := rank.HotPaths(store, args[0], queryLimit)
		if err != nil {
			return err
		}
		if queryJSON {
			return writeJSON(cmd, hits)
		}
		for _, h := range hits {
			fmt.Fprintf(cmd.OutOrStdout(), "  [count=%d %s] %s %s\n", h.Count, h.Dir, h.ID, h.Name)
		}
		return nil
	},
}

var queryDocsCmd = &cobra.Command{
	Use:   "docs <symbol>",
	Short: "Documentation tied to a symbol",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, cleanup, err := openMemberQueryStore()
		if err != nil {
			return err
		}
		defer cleanup()
		hits, err := rank.DocsForSymbol(store, args[0], queryRoot, queryLimit)
		if err != nil {
			return err
		}
		if queryJSON {
			return writeJSON(cmd, hits)
		}
		for _, h := range hits {
			fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s %s\n", h.Source, h.ID, truncateOneLine(h.Snippet, 100))
		}
		return nil
	},
}

func openMemberQueryStore() (graph.Store, func(), error) {
	if workspacePath != "" {
		opened, err := openWorkspaceStore(workspacePath, dataDir, repoName)
		if err != nil {
			return nil, nil, err
		}
		return opened.Store, func() { _ = opened.Closer.Close() }, nil
	}
	repo, err := resolveRepoName(queryRoot)
	if err != nil {
		return nil, nil, err
	}
	if repoName != "" {
		repo = repoName
	}
	store, err := badger.OpenWithRepo(dataDir, repo)
	if err != nil {
		return nil, nil, err
	}
	return store, func() { _ = store.Close() }, nil
}

func writeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runWorkspaceNeighborhood(cmd *cobra.Command, symbol string) error {
	opened, err := openWorkspaceStore(workspacePath, dataDir, repoName)
	if err != nil {
		return err
	}
	defer opened.Closer.Close()

	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	seed, err := rank.ResolveSeed(opened.Store, symbol)
	if err != nil {
		return err
	}
	opts := rank.Options{
		Depth:     queryDepth,
		MaxNodes:  queryMaxNodes,
		Budget:    queryBudget,
		RepoRoots: opened.Workspace.RepoRoots(),
		Explain:   queryExplain,
	}
	if repoName != "" {
		member, err := opened.Workspace.Lookup(repoName)
		if err != nil {
			return err
		}
		opts.RootDir = member.Path
	}
	res, err := rank.Neighborhood(opened.Store, seed, opts)
	if err != nil {
		return err
	}
	res.Warnings = append(res.Warnings, opened.Warnings...)
	if opened.Fed != nil {
		res.Warnings = append(res.Warnings, opened.Fed.TakeWarnings()...)
	}
	logWarnings(logger, res.Warnings)
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
		if queryExplain && len(h.Explain) > 0 {
			for _, p := range h.Explain {
				fmt.Fprintf(cmd.OutOrStdout(), "      explain %s %s w=%.3f (%s)\n", p.Family, p.Type, p.Weight, p.Dir)
			}
		}
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
	queryNeighborhoodCmd.Flags().BoolVar(&queryExplain, "explain", false, "Show per-family score breakdown")

	for _, c := range []*cobra.Command{queryCoChangesCmd, queryHotPathsCmd, queryDocsCmd} {
		c.Flags().StringVar(&queryRoot, "root", ".", "Repo root")
		c.Flags().BoolVar(&queryJSON, "json", false, "Emit JSON")
		c.Flags().IntVar(&queryLimit, "limit", 20, "Max results")
	}

	queryCmd.AddCommand(queryNeighborhoodCmd, queryCoChangesCmd, queryHotPathsCmd, queryDocsCmd)

	// Keep a helpful default when `synapse query` is invoked alone.
	queryCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	}
}
