package cli_test

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/keelwright-hq/synapse/internal/cli"
	"github.com/keelwright-hq/synapse/internal/store/badger"
	"github.com/keelwright-hq/synapse/internal/store/federated"
)

func workspaceFixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/cli → repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return filepath.Join(root, "testdata", "fixtures", "workspace")
}

func resetPersistentFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	if err := cmd.PersistentFlags().Set("workspace", ""); err != nil {
		t.Fatal(err)
	}
	if err := cmd.PersistentFlags().Set("repo", ""); err != nil {
		t.Fatal(err)
	}
	if err := cmd.PersistentFlags().Set("data-dir", ".synapse"); err != nil {
		t.Fatal(err)
	}
	// Local graph flags persist across Execute; clear between invocations.
	if f := cmd.Flags().Lookup("output"); f != nil {
		_ = f.Value.Set("-")
	}
	for _, name := range []string{"graph", "export"} {
		_ = name
	}
	if exportCmd, _, err := cmd.Find([]string{"graph", "export"}); err == nil {
		_ = exportCmd.Flags().Set("output", "-")
		_ = exportCmd.Flags().Set("overlay", "false")
	}
	if importCmd, _, err := cmd.Find([]string{"graph", "import"}); err == nil {
		_ = importCmd.Flags().Set("overlay", "false")
		_ = importCmd.Flags().Set("rewrite-repo", "false")
	}
}

func TestWorkspaceIndexAndQuery(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	dataDir := t.TempDir()

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{
		"index",
		"--workspace", wsDir,
		"--data-dir", dataDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index --workspace: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "indexed api:") || !strings.Contains(out, "indexed worker:") {
		t.Fatalf("expected per-repo index lines, got:\n%s", out)
	}

	apiStore, err := badger.OpenRepo(dataDir, "api")
	if err != nil {
		t.Fatal(err)
	}
	workerStore, err := badger.OpenRepo(dataDir, "worker")
	if err != nil {
		t.Fatal(err)
	}
	uAPI := "repo://api/svc/handler.go#func:Handle"
	uWorker := "repo://worker/svc/handler.go#func:Handle"
	if _, err := apiStore.GetNodeByURI(uAPI); err != nil {
		t.Fatalf("api uri: %v", err)
	}
	if _, err := workerStore.GetNodeByURI(uWorker); err != nil {
		t.Fatalf("worker uri: %v", err)
	}
	if err := apiStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := workerStore.Close(); err != nil {
		t.Fatal(err)
	}

	apiStore, err = badger.OpenRepo(dataDir, "api")
	if err != nil {
		t.Fatal(err)
	}
	workerStore, err = badger.OpenRepo(dataDir, "worker")
	if err != nil {
		t.Fatal(err)
	}
	fed, err := federated.New([]federated.Member{
		{Name: "api", Store: apiStore},
		{Name: "worker", Store: workerStore},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fed.GetNodeByURI(uAPI); err != nil {
		t.Fatal(err)
	}
	if _, err := fed.GetNodeByURI(uWorker); err != nil {
		t.Fatal(err)
	}
	_ = fed.Close()
	if err := apiStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := workerStore.Close(); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{
		"query", "neighborhood", "Handle",
		"--workspace", wsDir,
		"--data-dir", dataDir,
		"--repo", "api",
		"--json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("scoped query: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "func:svc/handler.go#Handle") {
		t.Fatalf("scoped query output:\n%s", buf.String())
	}

	buf.Reset()
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{
		"query", "neighborhood", uWorker,
		"--workspace", wsDir,
		"--data-dir", dataDir,
		"--json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("federated URI query: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "func:svc/handler.go#Handle") {
		t.Fatalf("federated query output:\n%s", buf.String())
	}

	buf.Reset()
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{
		"query", "neighborhood", "Handle",
		"--workspace", wsDir,
		"--data-dir", dataDir,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected ambiguous error, got success:\n%s", buf.String())
	} else if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguous error, got %v", err)
	}
}

func TestIndexWorkspaceRejectsPathArg(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"index", ".", "--workspace", wsDir, "--data-dir", t.TempDir()})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--workspace") {
		t.Fatalf("want path+workspace error, got %v", err)
	}
}
