package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/cli"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/badger"
)

func TestGraphExportImportRoundTrip(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"index", "--workspace", wsDir, "--data-dir", srcDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index: %v\n%s", err, buf.String())
	}

	apiSnap := filepath.Join(t.TempDir(), "api.ndjson")
	overlaySnap := filepath.Join(t.TempDir(), "overlay.ndjson")

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{"graph", "export", "--data-dir", srcDir, "--repo", "api", "-o", apiSnap})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("export api: %v\n%s", err, buf.String())
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{"graph", "export", "--data-dir", srcDir, "--overlay", "-o", overlaySnap})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("export overlay: %v\n%s", err, buf.String())
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{"graph", "import", "--data-dir", dstDir, "--repo", "api", apiSnap})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("import api: %v\n%s", err, buf.String())
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{"graph", "import", "--data-dir", dstDir, "--overlay", overlaySnap})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("import overlay: %v\n%s", err, buf.String())
	}

	store, err := badger.OpenRepo(dstDir, "api")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	op, err := store.GetNodeByURI("repo://api/users.proto#operation:UserService.ListUsers")
	if err != nil {
		t.Fatalf("imported operation: %v", err)
	}
	if op.Kind != parse.KindOperation {
		t.Fatalf("kind: %s", op.Kind)
	}
	handler, err := store.GetNodeByURI("repo://api/svc/handler.go#func:ListUsers")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := store.OutEdges(handler.ID, parse.EdgeImplements)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range edges {
		if e.To == op.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected implements edge after round-trip, edges=%+v", edges)
	}
}

func TestFederationExportImportCrossShard(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	snapDir := t.TempDir()

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"index", "--workspace", wsDir, "--data-dir", srcDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index: %v\n%s", err, buf.String())
	}

	for _, item := range []struct {
		args []string
		out  string
	}{
		{[]string{"graph", "export", "--data-dir", srcDir, "--repo", "api", "-o", filepath.Join(snapDir, "api.ndjson")}, filepath.Join(snapDir, "api.ndjson")},
		{[]string{"graph", "export", "--data-dir", srcDir, "--repo", "worker", "-o", filepath.Join(snapDir, "worker.ndjson")}, filepath.Join(snapDir, "worker.ndjson")},
		{[]string{"graph", "export", "--data-dir", srcDir, "--overlay", "-o", filepath.Join(snapDir, "overlay.ndjson")}, filepath.Join(snapDir, "overlay.ndjson")},
	} {
		resetPersistentFlags(t, cmd)
		buf.Reset()
		cmd.SetArgs(item.args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("export %v: %v\n%s", item.args, err, buf.String())
		}
	}

	for _, item := range [][]string{
		{"graph", "import", "--data-dir", dstDir, "--repo", "api", filepath.Join(snapDir, "api.ndjson")},
		{"graph", "import", "--data-dir", dstDir, "--repo", "worker", filepath.Join(snapDir, "worker.ndjson")},
		{"graph", "import", "--data-dir", dstDir, "--overlay", filepath.Join(snapDir, "overlay.ndjson")},
	} {
		resetPersistentFlags(t, cmd)
		buf.Reset()
		cmd.SetArgs(item)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("import %v: %v\n%s", item, err, buf.String())
		}
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{
		"query", "neighborhood", "repo://api/users.proto#operation:UserService.ListUsers",
		"--workspace", wsDir, "--data-dir", dstDir, "--json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("query: %v\n%s", err, buf.String())
	}
	var res rank.Result
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	foundConsume := false
	foundImpl := false
	for _, h := range res.Hits {
		if h.ID == "func:svc/handler.go#CallListUsers" && strings.Contains(h.EdgeReason, "consumes") {
			foundConsume = true
		}
		if h.ID == "func:svc/handler.go#ListUsers" && strings.Contains(h.EdgeReason, "implements") {
			foundImpl = true
		}
	}
	if !foundConsume || !foundImpl {
		t.Fatalf("expected cross-shard implements+consumes in hits=%+v", res.Hits)
	}
}

func TestFederationMissingShardWarning(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	snapDir := t.TempDir()

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"index", "--workspace", wsDir, "--data-dir", srcDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index: %v\n%s", err, buf.String()+errBuf.String())
	}

	apiSnap := filepath.Join(snapDir, "api.ndjson")
	overlaySnap := filepath.Join(snapDir, "overlay.ndjson")
	resetPersistentFlags(t, cmd)
	buf.Reset()
	errBuf.Reset()
	cmd.SetArgs([]string{"graph", "export", "--data-dir", srcDir, "--repo", "api", "-o", apiSnap})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"graph", "export", "--data-dir", srcDir, "--overlay", "-o", overlaySnap})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"graph", "import", "--data-dir", dstDir, "--repo", "api", apiSnap})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"graph", "import", "--data-dir", dstDir, "--overlay", overlaySnap})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// Worker shard intentionally absent.
	if badger.ShardExists(dstDir, "worker") {
		t.Fatal("worker shard should not exist")
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	errBuf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"query", "neighborhood", "repo://api/users.proto#operation:UserService.ListUsers",
		"--workspace", wsDir, "--data-dir", dstDir, "--json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("query: %v\n%s%s", err, buf.String(), errBuf.String())
	}
	var res rank.Result
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	joined := strings.Join(res.Warnings, "\n") + errBuf.String()
	if !strings.Contains(joined, "missing shard") && !strings.Contains(joined, "worker") {
		t.Fatalf("expected missing worker shard warning, warnings=%v stderr=%s", res.Warnings, errBuf.String())
	}
	// Partial success: api operation still resolves.
	if len(res.Hits) == 0 {
		t.Fatal("expected partial hits from api shard")
	}
}

func TestGraphImportRepoMismatch(t *testing.T) {
	wsDir := workspaceFixtureDir(t)
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	apiSnap := filepath.Join(t.TempDir(), "api.ndjson")

	cmd := cli.RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	resetPersistentFlags(t, cmd)
	cmd.SetArgs([]string{"index", "--workspace", wsDir, "--data-dir", srcDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index: %v\n%s", err, buf.String())
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{"graph", "export", "--data-dir", srcDir, "--repo", "api", "-o", apiSnap})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("export: %v\n%s", err, buf.String())
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{"graph", "import", "--data-dir", dstDir, "--repo", "renamed", apiSnap})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "does not match target") {
		t.Fatalf("want mismatch error, got %v\n%s", err, buf.String())
	}

	resetPersistentFlags(t, cmd)
	buf.Reset()
	cmd.SetArgs([]string{"graph", "import", "--data-dir", dstDir, "--repo", "renamed", "--rewrite-repo", apiSnap})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rewrite import: %v\n%s", err, buf.String())
	}

	store, err := badger.OpenRepo(dstDir, "renamed")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.GetNodeByURI("repo://renamed/svc/handler.go#func:ListUsers"); err != nil {
		t.Fatalf("rewritten uri: %v", err)
	}
	if _, err := store.GetNodeByURI("repo://api/svc/handler.go#func:ListUsers"); !errors.Is(err, graph.ErrNotFound) {
		t.Fatalf("old uri still present: %v", err)
	}
}
