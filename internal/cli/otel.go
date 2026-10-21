package cli

import (
	"fmt"

	"github.com/keelwright-hq/synapse/internal/config"
	"github.com/keelwright-hq/synapse/internal/otel"
	"github.com/keelwright-hq/synapse/internal/store/badger"
	"github.com/spf13/cobra"
)

var otelRoot string

var otelCmd = &cobra.Command{
	Use:   "otel",
	Short: "OpenTelemetry runtime evidence (Phase 3)",
	Long: `otel ingests runtime traces into observed_calls edges on the member
graph shard. Re-run after synapse index so file/symbol nodes exist.

Never writes to the contract overlay.`,
}

var otelIngestCmd = &cobra.Command{
	Use:   "ingest <trace.json>",
	Short: "Import a Synapse OTLP-ish JSON trace file into observed_calls edges",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spans, err := otel.ImportFile(args[0])
		if err != nil {
			return err
		}

		if workspacePath != "" {
			return runWorkspaceOtelIngest(cmd, spans)
		}

		root := "."
		if otelRoot != "" {
			root = otelRoot
		}
		repo, err := resolveRepoName(root)
		if err != nil {
			return err
		}
		if repoName != "" {
			repo = repoName
		}
		resolvedDataDir, err := resolveSingleRepoDataDir(cmd, root)
		if err != nil {
			return err
		}
		store, err := badger.OpenWithRepo(resolvedDataDir, repo)
		if err != nil {
			return err
		}
		defer store.Close()

		n, err := otel.Ingest(store, spans)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "otel ingest: upserted=%d edges (repo=%s data-dir=%s)\n", n, repo, resolvedDataDir)
		return nil
	},
}

func runWorkspaceOtelIngest(cmd *cobra.Command, spans []otel.Span) error {
	ws, err := config.Load(workspacePath)
	if err != nil {
		return err
	}
	if repoName != "" {
		member, err := ws.Lookup(repoName)
		if err != nil {
			return err
		}
		if !badger.ShardExists(dataDir, member.Name) {
			return fmt.Errorf("otel ingest: shard %q not found under %s", member.Name, dataDir)
		}
		store, err := badger.OpenRepo(dataDir, member.Name)
		if err != nil {
			return err
		}
		defer store.Close()
		n, err := otel.Ingest(store, spans)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "otel ingest: upserted=%d edges (repo=%s data-dir=%s)\n", n, member.Name, dataDir)
		return nil
	}

	total := 0
	openedAny := false
	for _, member := range ws.Repos {
		if !badger.ShardExists(dataDir, member.Name) {
			continue
		}
		openedAny = true
		store, err := badger.OpenRepo(dataDir, member.Name)
		if err != nil {
			return err
		}
		n, err := otel.Ingest(store, spans)
		_ = store.Close()
		if err != nil {
			return err
		}
		total += n
	}
	if !openedAny {
		return fmt.Errorf("otel ingest: no shards available under %s", dataDir)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "otel ingest: upserted=%d edges (workspace=%s data-dir=%s)\n", total, ws.ConfigPath, dataDir)
	return nil
}

func init() {
	otelIngestCmd.Flags().StringVar(&otelRoot, "root", ".", "Repo root used to resolve --repo default")
	otelCmd.AddCommand(otelIngestCmd)
	rootCmd.AddCommand(otelCmd)
}
