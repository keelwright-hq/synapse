package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/taricsa/synapse/internal/config"
	"github.com/taricsa/synapse/internal/contract/bind"
	"github.com/taricsa/synapse/internal/index"
	"github.com/taricsa/synapse/internal/store/badger"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a repository into the local code graph",
	Long: `Index walks source files, parses ASTs via tree-sitter, and persists
nodes and edges into the embedded graph store. Unchanged files are skipped
via content-hash fingerprints stored under --data-dir.

OpenAPI 3.x YAML/JSON specs are content-sniffed and mapped to operation/schema
nodes. After indexing, a heuristic binder links handlers/clients to operations
(implements/consumes).

With --workspace, indexes every member listed in synapse.yaml into
<data-dir>/repos/<name>/graph, then binds cross-repo contract edges into
<data-dir>/overlay. Do not pass a positional path in workspace mode.`,
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
		if err := bind.Bind(bind.Options{
			Members: []bind.Member{{Name: repo, Root: path, Store: store}},
		}); err != nil {
			return fmt.Errorf("index bind: %w", err)
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
