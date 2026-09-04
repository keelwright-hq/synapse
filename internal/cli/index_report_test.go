package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/cli"
	"github.com/keelwright-hq/synapse/internal/graph"
)

func TestIndexReportWritesArtifacts(t *testing.T) {
	repo := t.TempDir()
	writeTinyGoRepo(t, repo)
	t.Chdir(repo)

	clearDataDirFlag(t)
	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"index", ".", "--repo", "sample", "--report"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index --report: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "report written:") || !strings.Contains(out, "report latest:") {
		t.Fatalf("missing report lines:\n%s", out)
	}

	latest := filepath.Join(repo, ".synapse-out", "latest")
	for _, name := range []string{"manifest.json", "graph.json", "GRAPH_REPORT.md"} {
		if _, err := os.Stat(filepath.Join(latest, name)); err != nil {
			t.Fatalf("latest/%s: %v", name, err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(repo, ".synapse-out"))
	if err != nil {
		t.Fatal(err)
	}
	var runDirs int
	for _, e := range entries {
		if e.IsDir() && e.Name() != "latest" {
			runDirs++
			for _, name := range []string{"manifest.json", "graph.json", "GRAPH_REPORT.md"} {
				if _, err := os.Stat(filepath.Join(repo, ".synapse-out", e.Name(), name)); err != nil {
					t.Fatalf("run %s/%s: %v", e.Name(), name, err)
				}
			}
		}
	}
	if runDirs < 1 {
		t.Fatal("expected a run-id subdirectory under .synapse-out")
	}

	raw, err := os.ReadFile(filepath.Join(latest, "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nodes []graph.Node `json:"nodes"`
		Edges []graph.Edge `json:"edges"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) == 0 {
		t.Fatal("graph.json nodes empty")
	}
}

func TestIndexWithoutReportSkipsArtifacts(t *testing.T) {
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
		t.Fatalf("index: %v\n%s", err, buf.String())
	}
	if _, err := os.Stat(filepath.Join(repo, ".synapse-out")); !os.IsNotExist(err) {
		t.Fatalf("must not create .synapse-out without --report; err=%v", err)
	}
	if strings.Contains(buf.String(), "report written:") {
		t.Fatalf("unexpected report line:\n%s", buf.String())
	}
}

func TestIndexReportKeepsDistinctRunDirs(t *testing.T) {
	repo := t.TempDir()
	writeTinyGoRepo(t, repo)
	t.Chdir(repo)

	for i := 0; i < 2; i++ {
		clearDataDirFlag(t)
		cmd := cli.RootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"index", ".", "--repo", "sample", "--report"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("index --report #%d: %v\n%s", i+1, err, buf.String())
		}
	}

	entries, err := os.ReadDir(filepath.Join(repo, ".synapse-out"))
	if err != nil {
		t.Fatal(err)
	}
	var runDirs int
	for _, e := range entries {
		if e.IsDir() && e.Name() != "latest" {
			runDirs++
		}
	}
	if runDirs != 2 {
		t.Fatalf("want 2 historical run dirs, got %d", runDirs)
	}
}
