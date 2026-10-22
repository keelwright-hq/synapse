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

func TestOTLPJSONSpanIDAndFilepathDisambiguation(t *testing.T) {
	store := memory.New()
	_ = store.PutNode(graph.Node{ID: "file:a/handler.go", Kind: parse.KindFile, Name: "handler.go", Path: "a/handler.go"})
	_ = store.PutNode(graph.Node{ID: "func:a/handler.go#Handle", Kind: parse.KindFunction, Name: "Handle", Path: "a/handler.go"})
	_ = store.PutNode(graph.Node{ID: "file:b/handler.go", Kind: parse.KindFile, Name: "handler.go", Path: "b/handler.go"})
	_ = store.PutNode(graph.Node{ID: "func:b/handler.go#Handle", Kind: parse.KindFunction, Name: "Handle", Path: "b/handler.go"})

	dir := t.TempDir()
	path := filepath.Join(dir, "otlp.json")
	// Standard OTLP/JSON export with two spans sharing the same name "HTTP GET"
	// linked via spanId/parentSpanId and disambiguated via code.filepath + code.function.
	body := `{
  "resourceSpans": [
    {
      "resource": {
        "attributes": [
          {"key": "service.name", "value": {"stringValue": "gateway"}}
        ]
      },
      "scopeSpans": [
        {
          "spans": [
            {
              "spanId": "span-1",
              "name": "HTTP GET",
              "attributes": [
                {"key": "code.function", "value": {"stringValue": "Handle"}},
                {"key": "code.filepath", "value": {"stringValue": "a/handler.go"}}
              ]
            },
            {
              "spanId": "span-2",
              "parentSpanId": "span-1",
              "name": "HTTP GET",
              "attributes": [
                {"key": "code.function", "value": {"stringValue": "Handle"}},
                {"key": "code.filepath", "value": {"stringValue": "b/handler.go"}}
              ]
            }
          ]
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	spans, err := otel.ImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Fatalf("len(spans)=%d want 2", len(spans))
	}
	n, err := otel.Ingest(store, spans)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ingested=%d want 1", n)
	}
	edges, err := store.OutEdges("func:a/handler.go#Handle", parse.EdgeObservedCalls)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].To != "func:b/handler.go#Handle" {
		t.Fatalf("expected observed_calls func:a/handler.go#Handle -> func:b/handler.go#Handle, got %+v", edges)
	}
}

func TestResolveRelativeFilepathDoesNotMatchShorterRootFile(t *testing.T) {
	store := memory.New()
	_ = store.PutNode(graph.Node{ID: "file:main.go", Kind: parse.KindFile, Name: "main.go", Path: "main.go"})
	_ = store.PutNode(graph.Node{ID: "func:main.go#main", Kind: parse.KindFunction, Name: "main", Path: "main.go"})
	_ = store.PutNode(graph.Node{ID: "func:main.go#caller", Kind: parse.KindFunction, Name: "caller", Path: "main.go"})

	// Relative span path "cmd/api/main.go" must NOT match root "main.go".
	spRel := otel.Span{
		Name: "main",
		Attributes: map[string]string{
			"code.filepath": "cmd/api/main.go",
			"code.function": "main",
		},
	}
	if id, ok := otel.Resolve(store, spRel); ok {
		t.Fatalf("expected relative span path cmd/api/main.go not to resolve to root main.go, got %s", id)
	}
	n, err := otel.Ingest(store, []otel.Span{
		{SpanID: "1", Name: "caller", Attributes: map[string]string{"code.filepath": "main.go", "code.function": "caller"}},
		{SpanID: "2", ParentSpanID: "1", Name: "main", Attributes: map[string]string{"code.filepath": "cmd/api/main.go", "code.function": "main"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 ingested edges when child span filepath cmd/api/main.go is absent, got %d", n)
	}

	// Absolute span path "/app/cmd/api/main.go" still matches indexed "cmd/api/main.go".
	_ = store.PutNode(graph.Node{ID: "func:cmd/api/main.go#main", Kind: parse.KindFunction, Name: "main", Path: "cmd/api/main.go"})
	spAbs := otel.Span{
		Name: "main",
		Attributes: map[string]string{
			"code.filepath": "/app/cmd/api/main.go",
			"code.function": "main",
		},
	}
	if id, ok := otel.Resolve(store, spAbs); !ok || id != "func:cmd/api/main.go#main" {
		t.Fatalf("expected absolute span path /app/cmd/api/main.go to resolve to func:cmd/api/main.go#main, got %q (ok=%v)", id, ok)
	}
}
