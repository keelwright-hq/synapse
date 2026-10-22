package docs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/docs"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
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
	_ = store.PutNode(graph.Node{ID: "symbol:helper", Kind: parse.KindSymbol, Name: "helper"})
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
	var linkedHelper bool
	for _, e := range edges {
		if e.To == "func:greet.go#helper" {
			linkedHelper = true
		}
	}
	if !linkedHelper {
		t.Fatalf("expected documents edge to func:greet.go#helper, got %+v", edges)
	}

	hits, err := rank.DocsForSymbolWithRoots(store, "func:greet.go#helper", "", map[string]string{"demo": root}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, "See `helper` in greet.go") {
		t.Fatalf("expected non-empty doc snippet from edge, got %+v", hits)
	}
}

func TestDuplicateHeadingsAndStaleDocEdgePruning(t *testing.T) {
	root := t.TempDir()
	readme := filepath.Join(root, "README.md")
	body := "# Overview\n\nMentions `Alpha`.\n\n## Overview\n\nSecond overview section.\n"
	if err := os.WriteFile(readme, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := docs.ParseFile(readme, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	store := memory.New()
	headingURIs := map[string]bool{}
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
			if n.Kind == parse.KindHeading {
				headingURIs[canon] = true
			}
		}
		if err := store.PutNode(n); err != nil {
			t.Fatal(err)
		}
	}
	if len(headingURIs) != 2 {
		t.Fatalf("expected 2 distinct heading URIs for duplicate headings, got %v", headingURIs)
	}

	_ = store.PutNode(graph.Node{ID: "func:a.go#Alpha", Kind: parse.KindFunction, Name: "Alpha", Path: "a.go"})
	_ = store.PutNode(graph.Node{ID: "func:a.go#Beta", Kind: parse.KindFunction, Name: "Beta", Path: "a.go"})

	if _, err := docs.Link(docs.LinkOptions{Root: root, Store: store, Repo: "demo"}); err != nil {
		t.Fatal(err)
	}
	inAlpha, _ := store.InEdges("func:a.go#Alpha", parse.EdgeDocuments)
	if len(inAlpha) != 1 {
		t.Fatalf("expected 1 documents edge to Alpha, got %v", inAlpha)
	}

	// Update README.md to mention Beta instead of Alpha and re-link.
	if err := os.WriteFile(readme, []byte("# Overview\n\nMentions `Beta`.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := docs.Link(docs.LinkOptions{Root: root, Store: store, Repo: "demo"}); err != nil {
		t.Fatal(err)
	}
	inAlphaAfter, _ := store.InEdges("func:a.go#Alpha", parse.EdgeDocuments)
	if len(inAlphaAfter) != 0 {
		t.Fatalf("expected stale documents edge to Alpha to be pruned, got %v", inAlphaAfter)
	}
	inBetaAfter, _ := store.InEdges("func:a.go#Beta", parse.EdgeDocuments)
	if len(inBetaAfter) != 1 {
		t.Fatalf("expected 1 documents edge to Beta, got %v", inBetaAfter)
	}
}
