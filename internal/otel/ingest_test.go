package otel_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/otel"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestImportResolveIngest(t *testing.T) {
	store := memory.New()
	_ = store.PutNode(graph.Node{ID: "func:greet.go#main", Kind: parse.KindFunction, Name: "main", Path: "greet.go"})
	_ = store.PutNode(graph.Node{ID: "func:greet.go#helper", Kind: parse.KindFunction, Name: "helper", Path: "greet.go"})

	dir := t.TempDir()
	path := filepath.Join(dir, "trace.json")
	body := `{
  "spans": [
    {"name": "main", "attributes": {"code.function": "main"}},
    {"name": "helper", "parent": "main", "attributes": {"code.function": "helper"}}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	spans, err := otel.ImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n, err := otel.Ingest(store, spans)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ingested=%d want 1", n)
	}
	edges, err := store.OutEdges("func:greet.go#main", parse.EdgeObservedCalls)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].To != "func:greet.go#helper" {
		t.Fatalf("edges=%v", edges)
	}
	if edges[0].Props[parse.PropProvenance] != parse.ProvenanceObserved {
		t.Fatalf("props=%v", edges[0].Props)
	}
	// Second ingest increments count.
	_, _ = otel.Ingest(store, spans)
	edges, _ = store.OutEdges("func:greet.go#main", parse.EdgeObservedCalls)
	if edges[0].Props[parse.PropCount] != "2" {
		t.Fatalf("count=%s", edges[0].Props[parse.PropCount])
	}
}
