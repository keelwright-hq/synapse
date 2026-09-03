package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/taricsa/synapse/internal/config"
	"github.com/taricsa/synapse/internal/index"
	"github.com/taricsa/synapse/internal/store/badger"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a repository into the local code graph",
	Long: `Index walks source files, parses ASTs via tree-sitter, and persists
nodes and edges into the embedded graph store. Unchanged files are skipped
via content-hash fingerprints stored under --data-dir.

With --workspace, indexes every member listed in synapse.yaml into
<data-dir>/repos/<name>/graph. Do not pass a positional path in workspace mode.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if workspacePath != "" {
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
		store, err := badger.OpenWithRepo(dataDir, repo)
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
		fmt.Fprintf(cmd.OutOrStdout(),
			"index complete: processed=%d skipped=%d deleted=%d errors=%d (data-dir=%s repo=%s)\n",
			stats.Processed, stats.Skipped, stats.Deleted, stats.Errors, dataDir, repo)
		if stats.Errors > 0 {
			return fmt.Errorf("index finished with %d error(s)", stats.Errors)
		}
		return nil
	},
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
		total index.Stats
		failed int
	)
	for _, member := range ws.Repos {
		store, err := badger.OpenRepo(dataDir, member.Name)
		if err != nil {
			return fmt.Errorf("index workspace member %q: %w", member.Name, err)
		}
		stats, err := index.New(store).Run(member.Path, index.Options{Logger: logger, Repo: member.Name})
		_ = store.Close()
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
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"workspace index complete: repos=%d processed=%d skipped=%d deleted=%d errors=%d (data-dir=%s workspace=%s)\n",
		len(ws.Repos), total.Processed, total.Skipped, total.Deleted, total.Errors, dataDir, ws.ConfigPath)
	if total.Errors > 0 {
		return fmt.Errorf("workspace index finished with %d error(s) across %d repo(s)", total.Errors, failed)
	}
	return nil
}
