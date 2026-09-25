package docs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/docs"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
	"github.com/keelwright-hq/synapse/internal/uri"
)

func TestParseAndLink(t *testing.T) {
	root := t.TempDir()
	readme := filepath.Join(root, "README.md")
	body := "# Overview\n\nSee `helper` in greet.go for details.\n"
	if err := os.WriteFile(readme, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := docs.ParseFile(readme, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	var hasDoc, hasHeading bool
	for _, n := range res.Nodes {
		if n.Kind == parse.KindDoc {
			hasDoc = true
		}
		if n.Kind == parse.KindHeading && n.Name == "Overview" {
			hasHeading = true
		}
	}
	if !hasDoc || !hasHeading {
		t.Fatalf("nodes=%v", res.Nodes)
	}

	store := memory.New()
	for _, n := range res.Nodes {
		canon, ok, err := uri.Assign("demo", n.Path, n.Kind, n.Name, string(n.ID))
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			if n.Props == nil {
				n.Props = map[string]string{}
			}
			n.Props[uri.PropKey] = canon
		}
		if err := store.PutNode(n); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range res.Edges {
		if err := store.PutEdge(e); err != nil {
			t.Fatal(err)
		}
	}
	_ = store.PutNode(graph.Node{ID: "func:greet.go#helper", Kind: parse.KindFunction, Name: "helper", Path: "greet.go"})
	_ = store.PutNode(graph.Node{ID: "file:greet.go", Kind: parse.KindFile, Name: "greet.go", Path: "greet.go"})

	n, err := docs.Link(docs.LinkOptions{Root: root, Store: store, Repo: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected documents edges")
	}
	edges, err := store.OutEdges("doc:README.md#README", parse.EdgeDocuments)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Fatal("no documents edges from doc")
	}
}
