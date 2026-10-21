package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/cli"
)

func writeTinyGoRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package sample\n\nfunc Hello() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func clearDataDirFlag(t *testing.T) {
	t.Helper()
	cmd := cli.RootCommand()
	f := cmd.PersistentFlags().Lookup("data-dir")
	if f == nil {
		t.Fatal("missing data-dir flag")
	}
	if err := f.Value.Set(".synapse"); err != nil {
		t.Fatal(err)
	}
	f.Changed = false
	if err := cmd.PersistentFlags().Set("workspace", ""); err != nil {
		t.Fatal(err)
	}
	if err := cmd.PersistentFlags().Set("repo", ""); err != nil {
		t.Fatal(err)
	}
	if indexCmd, _, err := cmd.Find([]string{"index"}); err == nil {
		_ = indexCmd.Flags().Set("report", "false")
		_ = indexCmd.Flags().Set("report-dir", ".synapse-out")
	}
}

func TestIndexDefaultsDataDirInsideRepoCWD(t *testing.T) {
	repo := t.TempDir()
	writeTinyGoRepo(t, repo)
	t.Chdir(repo)

	clearDataDirFlag(t)
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", ".", "--repo", "sample"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index .: %v\n%s", err, buf.String())
	}

	want := filepath.Join(repo, ".synapse")
	out := buf.String()
	if !strings.Contains(out, "data-dir="+want) {
		t.Fatalf("want data-dir=%s in output, got:\n%s", want, out)
	}
	if _, err := os.Stat(filepath.Join(want, "graph")); err != nil {
		t.Fatalf("expected graph under repo .synapse: %v", err)
	}
}

func TestIndexDefaultsDataDirForAbsolutePathFromOutside(t *testing.T) {
	outer := t.TempDir()
	repo := filepath.Join(outer, "target-repo")
	writeTinyGoRepo(t, repo)
	cwd := filepath.Join(outer, "elsewhere")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	clearDataDirFlag(t)
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", repo, "--repo", "sample"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index abs: %v\n%s", err, buf.String())
	}

	want := filepath.Join(repo, ".synapse")
	out := buf.String()
	if !strings.Contains(out, "data-dir="+want) {
		t.Fatalf("want data-dir=%s in output, got:\n%s", want, out)
	}
	if _, err := os.Stat(filepath.Join(want, "graph")); err != nil {
		t.Fatalf("expected graph under target repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".synapse")); !os.IsNotExist(err) {
		t.Fatalf("must not create .synapse in outer cwd; err=%v", err)
	}
}

func TestIndexRespectsExplicitDataDir(t *testing.T) {
	outer := t.TempDir()
	repo := filepath.Join(outer, "target-repo")
	writeTinyGoRepo(t, repo)
	explicit := filepath.Join(outer, "explicit-data")
	t.Chdir(outer)

	clearDataDirFlag(t)
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", repo, "--repo", "sample", "--data-dir", explicit})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index with --data-dir: %v\n%s", err, buf.String())
	}

	absExplicit, err := filepath.Abs(explicit)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "data-dir="+absExplicit) {
		t.Fatalf("want data-dir=%s in output, got:\n%s", absExplicit, out)
	}
	if _, err := os.Stat(filepath.Join(absExplicit, "graph")); err != nil {
		t.Fatalf("expected graph under explicit data-dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".synapse")); !os.IsNotExist(err) {
		t.Fatalf("must not force .synapse into repo when --data-dir set; err=%v", err)
	}
}

func TestQueryAndOtelDefaultDataDirFromRootOutsideCWD(t *testing.T) {
	outer := t.TempDir()
	repo := filepath.Join(outer, "target-repo")
	writeTinyGoRepo(t, repo)
	cwd := filepath.Join(outer, "elsewhere")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	clearDataDirFlag(t)
	cmd.SetArgs([]string{"index", repo})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index: %v\n%s", err, buf.String())
	}

	// Query neighborhood using --root without --data-dir from outside CWD.
	buf.Reset()
	clearDataDirFlag(t)
	cmd.SetArgs([]string{"query", "neighborhood", "Hello", "--root", repo, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("query neighborhood with --root: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "func:hello.go#Hello") {
		t.Fatalf("expected func:hello.go#Hello in output, got:\n%s", buf.String())
	}

	// Ingest OTel trace using --root without --data-dir from outside CWD.
	traceFile := filepath.Join(outer, "trace.json")
	traceJSON := `{"spans":[{"name":"Hello","attributes":{"code.filepath":"hello.go","code.function":"Hello"}}]}`
	if err := os.WriteFile(traceFile, []byte(traceJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	clearDataDirFlag(t)
	cmd.SetArgs([]string{"otel", "ingest", traceFile, "--root", repo})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("otel ingest with --root: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), filepath.Join(repo, ".synapse")) {
		t.Fatalf("expected otel ingest to use repo .synapse, got:\n%s", buf.String())
	}
	if _, err := os.Stat(filepath.Join(cwd, ".synapse")); !os.IsNotExist(err) {
		t.Fatalf("must not create .synapse in outer cwd; err=%v", err)
	}
}

func TestOtelIngestSupportsWorkspaceMode(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	dataDir := t.TempDir()

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"index", "--workspace", wsDir, "--data-dir", dataDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index --workspace: %v\n%s", err, buf.String())
	}

	traceFile := filepath.Join(t.TempDir(), "trace.json")
	traceJSON := `{"spans":[
		{"name":"root","attributes":{"code.filepath":"svc/handler.go","code.function":"Handle"}},
		{"name":"child","parent":"root","attributes":{"code.filepath":"svc/handler.go","code.function":"Handle"}}
	]}`
	if err := os.WriteFile(traceFile, []byte(traceJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Scoped to one workspace member (--workspace + --repo).
	buf.Reset()
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"otel", "ingest", traceFile, "--workspace", wsDir, "--data-dir", dataDir, "--repo", "api"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("otel ingest --workspace --repo api: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "repo=api") {
		t.Fatalf("expected repo=api in output, got:\n%s", buf.String())
	}

	// Unscoped across all workspace shards (--workspace without --repo).
	buf.Reset()
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"otel", "ingest", traceFile, "--workspace", wsDir, "--data-dir", dataDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("otel ingest --workspace: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "workspace=") {
		t.Fatalf("expected workspace= in output, got:\n%s", buf.String())
	}
}
