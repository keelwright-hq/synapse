package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/taricsa/synapse/internal/index"
	"github.com/taricsa/synapse/internal/store/badger"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a repository into the local code graph",
	Long: `Index walks source files, parses ASTs via tree-sitter, and persists
nodes and edges into the embedded graph store. Unchanged files are skipped
via content-hash fingerprints stored under --data-dir.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
