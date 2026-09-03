package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// dataDir is the root directory for Synapse local state (default .synapse/).
var dataDir string

// rootCmd is the base command for the synapse CLI.
var rootCmd = &cobra.Command{
	Use:   "synapse",
	Short: "Go-native code context engine for AI IDEs",
	Long: `Synapse is an open-source code context engine written in Go.

It indexes repositories via tree-sitter, persists a code graph locally,
and serves context to AI IDEs over the Model Context Protocol (MCP).`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// RootCommand returns the root Cobra command (useful for tests).
func RootCommand() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", ".synapse", "Directory for local graph index data (single-repo index defaults to <repo>/.synapse when omitted)")
	rootCmd.PersistentFlags().StringVar(&repoName, "repo", "", "Canonical repo:// name (default: basename of index/query root); with --workspace scopes queries to one member")
	rootCmd.PersistentFlags().StringVar(&workspacePath, "workspace", "", "Path to synapse.yaml (or its directory) for multi-repo mode")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(queryCmd)
}
