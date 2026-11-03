package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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

	queryImpactDepth int
	queryImpactLimit int
	queryPathHops    int
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
		resolvedDataDir, err := resolveSingleRepoDataDir(cmd, queryRoot)
		if err != nil {
			return err
		}
		store, err := badger.OpenSingleOrRepo(resolvedDataDir, repo)
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
		store, cleanup, err := openMemberQueryStore(cmd)
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
		store, cleanup, err := openMemberQueryStore(cmd)
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
		store, repoRoots, cleanup, err := openMemberQueryStoreWithRoots(cmd)
		if err != nil {
			return err
		}
		defer cleanup()
		hits, err := rank.DocsForSymbolWithRoots(store, args[0], queryRoot, repoRoots, queryLimit)
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

func openMemberQueryStore(cmd *cobra.Command) (graph.Store, func(), error) {
	store, _, cleanup, err := openMemberQueryStoreWithRoots(cmd)
	return store, cleanup, err
}

func openMemberQueryStoreWithRoots(cmd *cobra.Command) (graph.Store, map[string]string, func(), error) {
	if workspacePath != "" {
		opened, err := openWorkspaceStore(workspacePath, dataDir, repoName)
		if err != nil {
			return nil, nil, nil, err
		}
		return opened.Store, opened.Workspace.RepoRoots(), func() { _ = opened.Closer.Close() }, nil
	}
	repo, err := resolveRepoName(queryRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	if repoName != "" {
		repo = repoName
	}
	resolvedDataDir, err := resolveSingleRepoDataDir(cmd, queryRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	store, err := badger.OpenSingleOrRepo(resolvedDataDir, repo)
	if err != nil {
		return nil, nil, nil, err
	}
	return store, nil, func() { _ = store.Close() }, nil
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

var queryAffectedCmd = &cobra.Command{
	Use:   "affected <symbol>",
	Short: "Code that calls, references or inherits from a symbol, grouped by file",
	Long: `affected walks calls, references, inherits and implements edges backwards
from a symbol. Every type on the walk includes its members, also those declared
in Swift extensions in other files, so callers of its methods count. A file:
seed covers everything the file declares.

--depth follows dependents of dependents. A file reached through an import at
depth 2 or more counts only its imports and top-level code, not everything it
declares, so deep queries on import-heavy code stay bounded.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runImpactQuery(cmd, args[0], rank.Affected)
	},
}

var queryDependenciesCmd = &cobra.Command{
	Use:   "dependencies <symbol>",
	Short: "Code a symbol calls, references or inherits from, grouped by file",
	Long: `dependencies walks calls, references, inherits and implements edges forwards
from a symbol (and a type's members).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runImpactQuery(cmd, args[0], rank.Dependencies)
	},
}

var queryPathCmd = &cobra.Command{
	Use:   "path <from> <to>",
	Short: "Shortest call/reference path between two symbols",
	Long: `path finds the fewest-hop chain from one symbol to another along calls,
references, inherits and implements edges in their own direction. A type target
is reached when the chain reaches any of its members.

Hops count every edge on the chain, including a "contains" step from a type
into one of its methods, so "3 hops" is not always three calls; the edge type
of each step is printed.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, cleanup, err := openMemberQueryStore(cmd)
		if err != nil {
			return err
		}
		defer cleanup()
		from, err := rank.ResolveSeed(store, args[0])
		if err != nil {
			return err
		}
		to, err := rank.ResolveSeed(store, args[1])
		if err != nil {
			return err
		}
		res, err := rank.ShortestPath(store, from, to, queryPathHops)
		if err != nil {
			return err
		}
		if queryJSON {
			return writeJSON(cmd, res)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "path %s -> %s (%d hops, edge type per step)\n", from, to, len(res.Steps)-1)
		for _, st := range res.Steps {
			edge := "start"
			if st.Edge != "" {
				edge = st.Edge
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %-10s %s %s:%d\n", edge, st.Name, st.Path, st.Line)
		}
		return nil
	},
}

func runImpactQuery(cmd *cobra.Command, symbol string, query func(graph.Store, graph.NodeID, int, int) (rank.ImpactResult, error)) error {
	var (
		store   graph.Store
		cleanup func()
		err     error
	)
	if workspacePath != "" {
		// The whole workspace, so the walk can cross API contracts into other
		// members; --repo only says which member the seed is in.
		opened, oerr := openWorkspaceStore(workspacePath, dataDir, "")
		if oerr != nil {
			return oerr
		}
		store, cleanup = opened.Store, func() { _ = opened.Closer.Close() }
	} else if store, cleanup, err = openMemberQueryStore(cmd); err != nil {
		return err
	}
	defer cleanup()
	seed, err := rank.ResolveSeedInRepo(store, repoName, symbol)
	if err != nil {
		return err
	}
	res, err := query(store, seed, queryImpactDepth, queryImpactLimit)
	if err != nil {
		return err
	}
	if queryJSON {
		return writeJSON(cmd, res)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "seed=%s files=%d symbols=%d truncated=%v\n", res.Seed, len(res.Files), res.Symbols, res.Truncated)
	// One line per file keeps the answer small; --json has every symbol.
	const perFile = 8
	for _, g := range res.Files {
		var parts []string
		for i, sym := range g.Symbols {
			if i == perFile {
				parts = append(parts, fmt.Sprintf("+%d more", len(g.Symbols)-perFile))
				break
			}
			if sym.Line > 0 {
				parts = append(parts, fmt.Sprintf("%s [%s] L%d", sym.Name, sym.Edge, sym.Line))
			} else {
				parts = append(parts, fmt.Sprintf("%s [%s]", sym.Name, sym.Edge)) // module-level code
			}
		}
		where := g.Path
		if g.Repo != "" {
			where = g.Repo + ":" + g.Path
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", where, strings.Join(parts, ", "))
	}
	return nil
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

	for _, c := range []*cobra.Command{queryAffectedCmd, queryDependenciesCmd, queryPathCmd} {
		c.Flags().StringVar(&queryRoot, "root", ".", "Repo root")
		c.Flags().BoolVar(&queryJSON, "json", false, "Emit JSON")
	}
	queryAffectedCmd.Flags().IntVar(&queryImpactDepth, "depth", 1, "Hops to follow (1 = direct dependents)")
	queryDependenciesCmd.Flags().IntVar(&queryImpactDepth, "depth", 1, "Hops to follow (1 = direct dependencies)")
	queryPathCmd.Flags().IntVar(&queryPathHops, "max-hops", 8, "Longest path to search")
	for _, c := range []*cobra.Command{queryAffectedCmd, queryDependenciesCmd} {
		c.Flags().IntVar(&queryImpactLimit, "limit", 100, "Max files")
	}

	queryCmd.AddCommand(queryNeighborhoodCmd, queryCoChangesCmd, queryHotPathsCmd, queryDocsCmd,
		queryAffectedCmd, queryDependenciesCmd, queryPathCmd)

	// Keep a helpful default when `synapse query` is invoked alone.
	queryCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	}
}
