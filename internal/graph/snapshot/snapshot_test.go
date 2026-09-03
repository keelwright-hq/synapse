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
