package cli

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/keelwright-hq/synapse/internal/config"
	"github.com/keelwright-hq/synapse/internal/contract/bind"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/report"
	"github.com/keelwright-hq/synapse/internal/store/badger"
	"github.com/spf13/cobra"
)

var (
	indexReport    bool
	indexReportDir string
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a repository into the local code graph",
	Long: `Index walks source files, parses ASTs via tree-sitter, and persists
nodes and edges into the embedded graph store. Unchanged files are skipped
via content-hash fingerprints stored under --data-dir.

For a single-repo index (no --workspace), when --data-dir is omitted the
default is <target-repo>/.synapse (the embedded Badger graph database under
the repository being indexed—not a human-readable report folder).

Pass --report to also write readable dry-run artifacts under
<target-repo>/.synapse-out/ (override with --report-dir): manifest.json,
graph.json, GRAPH_REPORT.md, and graph.html. See docs/report.md.

OpenAPI 3.x YAML/JSON specs are content-sniffed and mapped to operation/schema
nodes. After indexing, a heuristic binder links handlers/clients to operations
(implements/consumes).

With --workspace, indexes every member listed in synapse.yaml into
<data-dir>/repos/<name>/graph, then binds cross-repo contract edges into
<data-dir>/overlay. Do not pass a positional path in workspace mode.
Workspace mode keeps the --data-dir / CWD default unchanged.
--report is not supported with --workspace yet.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if workspacePath != "" {
			if indexReport {
				return fmt.Errorf("index: --report is not supported with --workspace yet")
			}
			if len(args) > 0 {
				return fmt.Errorf("index: do not pass a path with --workspace (members come from synapse.yaml)")
			}
			return runWorkspaceIndex(cmd)
		}

		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		repo, err := resolveRepoName(path)
		if err != nil {
			return err
		}
		resolvedDataDir, err := resolveSingleRepoDataDir(cmd, path)
		if err != nil {
			return err
		}
		store, err := badger.OpenWithRepo(resolvedDataDir, repo)
		if err != nil {
			return err
		}
		defer store.Close()

		logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		idx := index.New(store)
		stats, err := idx.Run(path, index.Options{Logger: logger, Repo: repo})
		if err != nil {
			return err
		}
		if err := bind.Bind(bind.Options{
			Members: []bind.Member{{Name: repo, Root: path, Store: store}},
		}); err != nil {
			return fmt.Errorf("index bind: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"index complete: processed=%d skipped=%d deleted=%d errors=%d (data-dir=%s repo=%s)\n",
			stats.Processed, stats.Skipped, stats.Deleted, stats.Errors, resolvedDataDir, repo)
		if stats.Errors > 0 {
			return fmt.Errorf("index finished with %d error(s)", stats.Errors)
		}
		if indexReport {
			if err := writeIndexReport(cmd.OutOrStdout(), path, repo, stats, store); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	indexCmd.Flags().BoolVar(&indexReport, "report", false, "Write readable artifacts under --report-dir (single-repo only)")
	indexCmd.Flags().StringVar(&indexReportDir, "report-dir", ".synapse-out", "Readable artifact directory relative to the target repo (or absolute)")
}

// resolveSingleRepoDataDir returns the Badger root for a single-repo index.
// When --data-dir was not explicitly set, it defaults to <abs(root)>/.synapse.
// Explicit --data-dir values are preserved (absolutized only for clear logging).
func resolveSingleRepoDataDir(cmd *cobra.Command, root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("index: resolve path: %w", err)
	}
	dir := dataDir
	if !cmd.Flags().Changed("data-dir") {
		dir = filepath.Join(absRoot, ".synapse")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("index: resolve data-dir: %w", err)
	}
	return absDir, nil
}

func writeIndexReport(w io.Writer, root, repo string, stats index.Stats, store *badger.Store) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("index report: resolve root: %w", err)
	}
	reportRoot := indexReportDir
	if reportRoot == "" {
		reportRoot = ".synapse-out"
	}
	if !filepath.IsAbs(reportRoot) {
		reportRoot = filepath.Join(absRoot, reportRoot)
	}
	runID := report.NewRunID()
	runDir := filepath.Join(reportRoot, runID)
	latestDir := filepath.Join(reportRoot, "latest")

	res, err := report.Write(report.Options{
		Repo:   repo,
		Root:   absRoot,
		OutDir: runDir,
		Stats:  stats,
		Store:  store,
	})
	if err != nil {
		return fmt.Errorf("index report: %w", err)
	}
	if err := report.CopyArtifacts(runDir, latestDir); err != nil {
		return fmt.Errorf("index report: refresh latest: %w", err)
	}
	fmt.Fprintf(w, "report written: %s\n", res.ReportPath)
	fmt.Fprintf(w, "report latest:  %s\n", filepath.Join(latestDir, "GRAPH_REPORT.md"))
	return nil
}

func runWorkspaceIndex(cmd *cobra.Command) error {
	ws, err := config.Load(workspacePath)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	var (
		total  index.Stats
		failed int
	)
	members := make([]bind.Member, 0, len(ws.Repos))
	stores := make([]*badger.Store, 0, len(ws.Repos))
	defer func() {
		for _, s := range stores {
			_ = s.Close()
		}
	}()

	for _, member := range ws.Repos {
		store, err := badger.OpenRepo(dataDir, member.Name)
		if err != nil {
			return fmt.Errorf("index workspace member %q: %w", member.Name, err)
		}
		stores = append(stores, store)
		stats, err := index.New(store).Run(member.Path, index.Options{Logger: logger, Repo: member.Name})
		if err != nil {
			return fmt.Errorf("index workspace member %q: %w", member.Name, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"indexed %s: processed=%d skipped=%d deleted=%d errors=%d\n",
			member.Name, stats.Processed, stats.Skipped, stats.Deleted, stats.Errors)
		total.Processed += stats.Processed
		total.Skipped += stats.Skipped
		total.Deleted += stats.Deleted
		total.Errors += stats.Errors
		if stats.Errors > 0 {
			failed++
		}
		members = append(members, bind.Member{
			Name:  member.Name,
			Root:  member.Path,
			Store: store,
		})
	}

	overlay, err := badger.OpenOverlay(dataDir)
	if err != nil {
		return fmt.Errorf("index workspace overlay: %w", err)
	}
	defer overlay.Close()

	if err := bind.Bind(bind.Options{Members: members, Overlay: overlay}); err != nil {
		return fmt.Errorf("index workspace bind: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(),
		"workspace index complete: repos=%d processed=%d skipped=%d deleted=%d errors=%d (data-dir=%s workspace=%s)\n",
		len(ws.Repos), total.Processed, total.Skipped, total.Deleted, total.Errors, dataDir, ws.ConfigPath)
	if total.Errors > 0 {
		return fmt.Errorf("workspace index finished with %d error(s) across %d repo(s)", total.Errors, failed)
	}
	return nil
}
