package snapshot_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/graph/snapshot"
	"github.com/taricsa/synapse/internal/parse"
	"github.com/taricsa/synapse/internal/store/memory"
	"github.com/taricsa/synapse/internal/uri"
)

func TestRoundTripMemory(t *testing.T) {
	src := memory.New()
	n1 := graph.Node{
		ID:   "func:a.go#A",
		Kind: parse.KindFunction,
		Name: "A",
		Path: "a.go",
		Props: map[string]string{
			uri.PropKey: "repo://demo/a.go#func:A",
		},
	}
	n2 := graph.Node{
		ID:   "func:a.go#B",
		Kind: parse.KindFunction,
		Name: "B",
		Path: "a.go",
		Props: map[string]string{
			uri.PropKey: "repo://demo/a.go#func:B",
		},
	}
	if err := src.PutNode(n1); err != nil {
		t.Fatal(err)
	}
	if err := src.PutNode(n2); err != nil {
		t.Fatal(err)
	}
	if err := src.PutEdge(graph.Edge{From: n1.ID, To: n2.ID, Type: parse.EdgeCalls}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := snapshot.Export(&buf, src, snapshot.Meta{Repo: "demo", Kind: snapshot.KindRepo}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"format":"synapse.graph.snapshot"`) {
		t.Fatalf("missing format header: %s", out)
	}
	if !strings.Contains(out, `"edge_type":"calls"`) {
		t.Fatalf("missing edge_type: %s", out)
	}

	dst := memory.New()
	res, err := snapshot.Import(bytes.NewReader(buf.Bytes()), dst)
	if err != nil {
		t.Fatal(err)
	}
	if res.Nodes != 2 || res.Edges != 1 {
		t.Fatalf("counts: nodes=%d edges=%d", res.Nodes, res.Edges)
	}
	if res.Meta.Repo != "demo" {
		t.Fatalf("meta repo: %q", res.Meta.Repo)
	}

	got, err := dst.GetNodeByURI("repo://demo/a.go#func:A")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "A" {
		t.Fatalf("node: %+v", got)
	}
	edges, err := dst.OutEdges(n1.ID, parse.EdgeCalls)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].To != n2.ID {
		t.Fatalf("edges: %+v", edges)
	}
}

func TestImportRejectsBadVersion(t *testing.T) {
	body := `{"type":"header","format":"synapse.graph.snapshot","version":99,"repo":"x","kind":"repo"}` + "\n"
	_, err := snapshot.Import(strings.NewReader(body), memory.New())
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("want version error, got %v", err)
	}
}

func TestImportRejectsMissingHeader(t *testing.T) {
	body := `{"type":"node","id":"a","kind":"file"}` + "\n"
	_, err := snapshot.Import(strings.NewReader(body), memory.New())
	if err == nil || !strings.Contains(err.Error(), "before header") {
		t.Fatalf("want header error, got %v", err)
	}
}

func TestImportRejectsRepoMismatch(t *testing.T) {
	src := memory.New()
	n := graph.Node{
		ID:   "func:a.go#A",
		Kind: parse.KindFunction,
		Name: "A",
		Path: "a.go",
		Props: map[string]string{
			uri.PropKey: "repo://demo/a.go#func:A",
		},
	}
	if err := src.PutNode(n); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := snapshot.Export(&buf, src, snapshot.Meta{Repo: "demo", Kind: snapshot.KindRepo}); err != nil {
		t.Fatal(err)
	}
	dst := memory.New()
	_, err := snapshot.ImportWithOptions(bytes.NewReader(buf.Bytes()), dst, snapshot.ImportOptions{
		TargetRepo: "renamed",
	})
	if err == nil || !strings.Contains(err.Error(), "does not match target") {
		t.Fatalf("want mismatch error, got %v", err)
	}
	if ncount := countNodes(t, dst); ncount != 0 {
		t.Fatalf("mismatch import wrote %d nodes", ncount)
	}
}

func TestImportRewriteRepoURI(t *testing.T) {
	src := memory.New()
	n1 := graph.Node{
		ID:   "func:a.go#A",
		Kind: parse.KindFunction,
		Name: "A",
		Path: "a.go",
		Props: map[string]string{
			uri.PropKey: "repo://demo/a.go#func:A",
		},
	}
	n2 := graph.Node{
		ID:   "func:a.go#B",
		Kind: parse.KindFunction,
		Name: "B",
		Path: "a.go",
		Props: map[string]string{
			uri.PropKey: "repo://demo/a.go#func:B",
		},
	}
	opURI := "repo://demo/openapi.yaml#operation:GET /users"
	canonicalOp, err := uri.Normalize(opURI)
	if err != nil {
		t.Fatal(err)
	}
	overlayN := graph.Node{
		ID:   graph.NodeID(canonicalOp),
		Kind: parse.KindOperation,
		Name: "GET /users",
		Path: "openapi.yaml",
		Props: map[string]string{
			uri.PropKey: canonicalOp,
		},
	}
	if err := src.PutNode(n1); err != nil {
		t.Fatal(err)
	}
	if err := src.PutNode(n2); err != nil {
		t.Fatal(err)
	}
	if err := src.PutNode(overlayN); err != nil {
		t.Fatal(err)
	}
	if err := src.PutEdge(graph.Edge{From: n1.ID, To: n2.ID, Type: parse.EdgeCalls}); err != nil {
		t.Fatal(err)
	}
	if err := src.PutEdge(graph.Edge{
		From: overlayN.ID, To: n2.ID, Type: parse.EdgeImplements,
		Props: map[string]string{uri.PropKey: canonicalOp},
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := snapshot.Export(&buf, src, snapshot.Meta{Repo: "demo", Kind: snapshot.KindRepo}); err != nil {
		t.Fatal(err)
	}

	dst := memory.New()
	res, err := snapshot.ImportWithOptions(bytes.NewReader(buf.Bytes()), dst, snapshot.ImportOptions{
		TargetRepo:  "renamed",
		RewriteRepo: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Meta.Repo != "renamed" {
		t.Fatalf("meta repo: %q", res.Meta.Repo)
	}

	got, err := dst.GetNodeByURI("repo://renamed/a.go#func:A")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != n1.ID {
		t.Fatalf("phase-1 id changed: %s", got.ID)
	}
	if _, err := dst.GetNodeByURI("repo://demo/a.go#func:A"); err == nil {
		t.Fatal("old repo_uri still indexed")
	}
	edges, err := dst.OutEdges(n1.ID, parse.EdgeCalls)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].To != n2.ID {
		t.Fatalf("phase-1 edges: %+v", edges)
	}
	wantOverlay, err := uri.RewriteRepo(canonicalOp, "demo", "renamed")
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := dst.GetNode(graph.NodeID(wantOverlay))
	if err != nil {
		t.Fatal(err)
	}
	if rewritten.Props[uri.PropKey] != wantOverlay {
		t.Fatalf("overlay node props: %+v", rewritten.Props)
	}
	ovEdges, err := dst.OutEdges(rewritten.ID, parse.EdgeImplements)
	if err != nil {
		t.Fatal(err)
	}
	if len(ovEdges) != 1 || ovEdges[0].To != n2.ID {
		t.Fatalf("uri-keyed edges: %+v", ovEdges)
	}
	if ovEdges[0].Props[uri.PropKey] != wantOverlay {
		t.Fatalf("edge prop: %+v", ovEdges[0].Props)
	}
}

func countNodes(t *testing.T, store graph.Store) int {
	t.Helper()
	n := 0
	if err := store.ForEachNode(func(graph.Node) bool {
		n++
		return true
	}); err != nil {
		t.Fatal(err)
	}
	return n
}
