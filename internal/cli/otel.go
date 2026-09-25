package cli

import (
	"fmt"

	"github.com/keelwright-hq/synapse/internal/otel"
	"github.com/keelwright-hq/synapse/internal/store/badger"
	"github.com/spf13/cobra"
)

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
		if workspacePath != "" {
			return fmt.Errorf("otel ingest: use --repo with a member shard (not --workspace federated view)")
		}
		root := "."
		if queryRoot != "" {
			root = queryRoot
		}
		repo, err := resolveRepoName(root)
		if err != nil {
			return err
		}
		if repoName != "" {
			repo = repoName
		}
		store, err := badger.OpenWithRepo(dataDir, repo)
		if err != nil {
			return err
		}
		defer store.Close()

		spans, err := otel.ImportFile(args[0])
		if err != nil {
			return err
		}
		n, err := otel.Ingest(store, spans)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "otel ingest: upserted=%d edges (repo=%s data-dir=%s)\n", n, repo, dataDir)
		return nil
	},
}

func init() {
	otelIngestCmd.Flags().StringVar(&queryRoot, "root", ".", "Repo root used to resolve --repo default")
	otelCmd.AddCommand(otelIngestCmd)
	rootCmd.AddCommand(otelCmd)
}
