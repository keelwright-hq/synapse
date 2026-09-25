package git_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	synapsegit "github.com/keelwright-hq/synapse/internal/git"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestWalkAggregateAnalyze(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("a.go", "package a\n")
	mustWrite("b.go", "package b\n")
	mustWrite("c.go", "package c\n")

	r, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commitPair := func(msg string, files ...string) {
		t.Helper()
		for _, f := range files {
			// Touch so the commit is not empty on repeat.
			p := filepath.Join(root, f)
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Add(f); err != nil {
				t.Fatal(err)
			}
		}
		_, err := w.Commit(msg, &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	commitPair("ab1", "a.go", "b.go")
	commitPair("ab2", "a.go", "b.go")
	commitPair("ac1", "a.go", "c.go")
	commitPair("ac2", "a.go", "c.go")

	commits, err := synapsegit.Walk(root, synapsegit.Options{MaxCommits: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) < 2 {
		t.Fatalf("want >=2 commits with multi-file sets, got %d", len(commits))
	}
	pairs := synapsegit.Aggregate(commits, synapsegit.Options{MinSupport: 2})
	if len(pairs) == 0 {
		t.Fatal("expected aggregated pairs")
	}

	store := memory.New()
	for _, p := range []string{"a.go", "b.go", "c.go"} {
		id := graph.NodeID("file:" + p)
		if err := store.PutNode(graph.Node{ID: id, Kind: parse.KindFile, Name: p, Path: p}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := synapsegit.Analyze(store, root, synapsegit.Options{MaxCommits: 50, MinSupport: 2})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected co_committed edges")
	}
	edges, err := synapsegit.TopCoChanges(store, "a.go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Fatal("TopCoChanges empty")
	}
	if edges[0].Type != parse.EdgeCoCommitted {
		t.Fatalf("type=%s", edges[0].Type)
	}
	if edges[0].Props[parse.PropProvenance] != parse.ProvenanceHistorical {
		t.Fatalf("provenance=%v", edges[0].Props)
	}
}
