package rank_test

import (
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestFamilyWeightsReorderNeighborhood(t *testing.T) {
	store := memory.New()
	seed := graph.NodeID("func:a.go#seed")
	staticN := graph.NodeID("func:a.go#static")
	gitN := graph.NodeID("file:b.go")
	for _, n := range []graph.Node{
		{ID: seed, Kind: parse.KindFunction, Name: "seed", Path: "a.go"},
		{ID: staticN, Kind: parse.KindFunction, Name: "static", Path: "a.go"},
		{ID: graph.NodeID("file:a.go"), Kind: parse.KindFile, Name: "a.go", Path: "a.go"},
		{ID: gitN, Kind: parse.KindFile, Name: "b.go", Path: "b.go"},
	} {
		if err := store.PutNode(n); err != nil {
			t.Fatal(err)
		}
	}
	_ = store.PutEdge(graph.Edge{From: seed, To: staticN, Type: parse.EdgeCalls})
	_ = store.PutEdge(graph.Edge{From: graph.NodeID("file:a.go"), To: gitN, Type: parse.EdgeCoCommitted, Props: map[string]string{parse.PropWeight: "1"}})
	_ = store.PutEdge(graph.Edge{From: seed, To: graph.NodeID("file:a.go"), Type: parse.EdgeContains})

	base, err := rank.Neighborhood(store, seed, rank.Options{Depth: 2, MaxNodes: 10})
	if err != nil {
		t.Fatal(err)
	}
	boosted, err := rank.Neighborhood(store, seed, rank.Options{
		Depth: 2, MaxNodes: 10,
		FamilyWeights: map[rank.EdgeFamily]float64{
			rank.FamilyGit:    5.0,
			rank.FamilyStatic: 0.1,
		},
		Explain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Hits) < 2 || len(boosted.Hits) < 2 {
		t.Fatalf("hits base=%d boosted=%d", len(base.Hits), len(boosted.Hits))
	}
	// With git boosted, file:b.go should outrank the static call neighbor among non-seed hits.
	var boostGitScore, boostStaticScore float64
	for _, h := range boosted.Hits {
		switch h.ID {
		case gitN:
			boostGitScore = h.Score
			if len(h.Explain) == 0 {
				t.Fatal("expected explain parts")
			}
		case staticN:
			boostStaticScore = h.Score
		}
	}
	if boostGitScore == 0 || boostStaticScore == 0 {
		t.Fatalf("missing scores git=%v static=%v", boostGitScore, boostStaticScore)
	}
	if boostGitScore <= boostStaticScore {
		t.Fatalf("expected git-boosted score > static; git=%v static=%v", boostGitScore, boostStaticScore)
	}
	_ = rank.WeightsFromFamilies(rank.DefaultFamilyWeights)
}

func TestSemanticQueries(t *testing.T) {
	store := memory.New()
	a := graph.NodeID("file:a.go")
	b := graph.NodeID("file:b.go")
	fn := graph.NodeID("func:a.go#helper")
	doc := graph.NodeID("doc:README.md#README")
	for _, n := range []graph.Node{
		{ID: a, Kind: parse.KindFile, Name: "a.go", Path: "a.go"},
		{ID: b, Kind: parse.KindFile, Name: "b.go", Path: "b.go"},
		{ID: fn, Kind: parse.KindFunction, Name: "helper", Path: "a.go", Props: map[string]string{parse.PropDoc: "Helper docs"}},
		{ID: doc, Kind: parse.KindDoc, Name: "README", Path: "README.md"},
	} {
		_ = store.PutNode(n)
	}
	_ = store.PutNode(graph.Node{ID: "func:a.go#other", Kind: parse.KindFunction, Name: "other", Path: "a.go"})
	_ = store.PutEdge(graph.Edge{From: a, To: b, Type: parse.EdgeCoCommitted, Props: map[string]string{parse.PropWeight: "2.5", parse.PropSupport: "3"}})
	_ = store.PutEdge(graph.Edge{From: fn, To: graph.NodeID("func:a.go#other"), Type: parse.EdgeObservedCalls, Props: map[string]string{parse.PropCount: "9", parse.PropProvenance: parse.ProvenanceObserved}})
	_ = store.PutEdge(graph.Edge{From: doc, To: fn, Type: parse.EdgeDocuments})

	cc, err := rank.CoChanges(store, "a.go", 10)
	if err != nil || len(cc) != 1 {
		t.Fatalf("cochanges=%v err=%v", cc, err)
	}
	hp, err := rank.HotPaths(store, string(fn), 10)
	if err != nil || len(hp) != 1 || hp[0].Count != 9 {
		t.Fatalf("hotpaths=%v err=%v", hp, err)
	}
	docs, err := rank.DocsForSymbol(store, string(fn), "", 10)
	if err != nil || len(docs) < 2 {
		t.Fatalf("docs=%v err=%v", docs, err)
	}
}
