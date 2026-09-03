package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/taricsa/synapse/internal/graph/snapshot"
	"github.com/taricsa/synapse/internal/store/badger"
	"github.com/taricsa/synapse/internal/uri"
)

var (
	graphExportOut     string
	graphExportOverlay bool
	graphImportOverlay bool
	graphImportRewrite bool
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Export and import graph shard snapshots",
	Long: `graph moves Synapse Badger shards as versioned NDJSON snapshots (SYN-16).

Use export to serialize a repo shard or the workspace overlay; import to load
a snapshot into --data-dir. See docs/federation.md.`,
}

var graphExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a graph shard to NDJSON",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphExportOverlay && repoName != "" {
			return fmt.Errorf("graph export: use either --overlay or --repo, not both")
		}
		if !graphExportOverlay && repoName == "" {
			return fmt.Errorf("graph export: require --repo NAME or --overlay")
		}

		var (
			store *badger.Store
			meta  snapshot.Meta
			err   error
		)
		if graphExportOverlay {
			store, err = badger.OpenOverlay(dataDir)
			meta = snapshot.Meta{Kind: snapshot.KindOverlay}
		} else {
			name, nerr := uri.NormalizeRepo(repoName)
			if nerr != nil {
				return nerr
			}
			store, err = badger.OpenRepo(dataDir, name)
			meta = snapshot.Meta{Repo: name, Kind: snapshot.KindRepo}
		}
		if err != nil {
			return err
		}
		defer store.Close()

		out := cmd.OutOrStdout()
		var closer io.Closer
		if graphExportOut != "" && graphExportOut != "-" {
			f, err := os.Create(graphExportOut)
			if err != nil {
				return err
			}
			closer = f
			out = f
		}
		if closer != nil {
			defer closer.Close()
		}

		if err := snapshot.Export(out, store, meta); err != nil {
			return err
		}
		if graphExportOut != "" && graphExportOut != "-" {
			fmt.Fprintf(cmd.ErrOrStderr(), "exported snapshot to %s\n", graphExportOut)
		}
		return nil
	},
}

var graphImportCmd = &cobra.Command{
	Use:   "import [file|-]",
	Short: "Import an NDJSON graph snapshot",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphImportOverlay && repoName != "" {
			return fmt.Errorf("graph import: use either --overlay or --repo, not both")
		}
		if !graphImportOverlay && repoName == "" {
			return fmt.Errorf("graph import: require --repo NAME or --overlay")
		}
		if graphImportRewrite && graphImportOverlay {
			return fmt.Errorf("graph import: --rewrite-repo is not valid with --overlay")
		}

		in := cmd.InOrStdin()
		var closer io.Closer
		path := args[0]
		if path != "-" {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			closer = f
			in = f
		}
		if closer != nil {
			defer closer.Close()
		}

		var (
			store      *badger.Store
			err        error
			targetRepo string
		)
		if graphImportOverlay {
			store, err = badger.OpenOverlay(dataDir)
		} else {
			name, nerr := uri.NormalizeRepo(repoName)
			if nerr != nil {
				return nerr
			}
			store, err = badger.OpenRepo(dataDir, name)
			targetRepo = name
		}
		if err != nil {
			return err
		}
		defer store.Close()

		res, err := snapshot.ImportWithOptions(in, store, snapshot.ImportOptions{
			TargetRepo:  targetRepo,
			RewriteRepo: graphImportRewrite,
		})
		if err != nil {
			return err
		}
		if graphImportOverlay {
			if res.Meta.Kind != snapshot.KindOverlay {
				return fmt.Errorf("graph import: snapshot kind is %q, expected overlay", res.Meta.Kind)
			}
		} else if res.Meta.Kind != snapshot.KindRepo {
			return fmt.Errorf("graph import: snapshot kind is %q, expected repo", res.Meta.Kind)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"imported snapshot: nodes=%d edges=%d kind=%s repo=%s (data-dir=%s)\n",
			res.Nodes, res.Edges, res.Meta.Kind, res.Meta.Repo, dataDir)
		return nil
	},
}

func init() {
	graphExportCmd.Flags().StringVarP(&graphExportOut, "output", "o", "-", "Output file (default stdout)")
	graphExportCmd.Flags().BoolVar(&graphExportOverlay, "overlay", false, "Export the workspace overlay store")
	graphImportCmd.Flags().BoolVar(&graphImportOverlay, "overlay", false, "Import into the workspace overlay store")
	graphImportCmd.Flags().BoolVar(&graphImportRewrite, "rewrite-repo", false, "Remap snapshot repo_uri values to --repo when names differ")
	graphCmd.AddCommand(graphExportCmd)
	graphCmd.AddCommand(graphImportCmd)
	rootCmd.AddCommand(graphCmd)
}
