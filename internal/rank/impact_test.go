package rank_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

// impactFixture is a small Swift-shaped graph:
//
//	Foo.swift      type Foo (declaration), module "Foo"
//	Foo+Ext.swift  extension Foo { func bar() }  -> bar calls Leaf.make
//	Mid.swift      type Mid references Leaf; Mid.run is a member
//	Top.swift      Top.go calls Mid.run; Top.useBar calls Foo.bar
//	Leaf.swift     type Leaf with Leaf.make
func impactFixture(t *testing.T) graph.Store {
	t.Helper()
	s := memory.New()
	ext := map[string]string{parse.PropExtension: "true"}
	for _, n := range []graph.Node{
		{ID: "module:Foo.swift", Kind: parse.KindModule, Name: "Foo", Path: "Foo.swift"},
		{ID: "type:Foo.swift#Foo", Kind: parse.KindType, Name: "Foo", Path: "Foo.swift"},
		{ID: "type:Foo+Ext.swift#Foo", Kind: parse.KindType, Name: "Foo", Path: "Foo+Ext.swift", Props: ext},
		{ID: "method:Foo+Ext.swift#Foo.bar", Kind: parse.KindMethod, Name: "bar", Path: "Foo+Ext.swift"},
		{ID: "type:Mid.swift#Mid", Kind: parse.KindType, Name: "Mid", Path: "Mid.swift"},
		{ID: "method:Mid.swift#Mid.run", Kind: parse.KindMethod, Name: "run", Path: "Mid.swift"},
		{ID: "type:Top.swift#Top", Kind: parse.KindType, Name: "Top", Path: "Top.swift"},
		{ID: "method:Top.swift#Top.go", Kind: parse.KindMethod, Name: "go", Path: "Top.swift"},
		{ID: "method:Top.swift#Top.useBar", Kind: parse.KindMethod, Name: "useBar", Path: "Top.swift"},
		{ID: "type:Leaf.swift#Leaf", Kind: parse.KindType, Name: "Leaf", Path: "Leaf.swift"},
		{ID: "method:Leaf.swift#Leaf.make", Kind: parse.KindMethod, Name: "make", Path: "Leaf.swift"},
	} {
		if err := s.PutNode(n); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []graph.Edge{
		{From: "type:Foo+Ext.swift#Foo", To: "method:Foo+Ext.swift#Foo.bar", Type: parse.EdgeContains},
		{From: "method:Foo+Ext.swift#Foo.bar", To: "method:Leaf.swift#Leaf.make", Type: parse.EdgeCalls},
		{From: "type:Mid.swift#Mid", To: "method:Mid.swift#Mid.run", Type: parse.EdgeContains},
		{From: "type:Mid.swift#Mid", To: "type:Leaf.swift#Leaf", Type: parse.EdgeReferences},
		{From: "type:Top.swift#Top", To: "method:Top.swift#Top.go", Type: parse.EdgeContains},
		{From: "type:Top.swift#Top", To: "method:Top.swift#Top.useBar", Type: parse.EdgeContains},
		{From: "method:Top.swift#Top.go", To: "method:Mid.swift#Mid.run", Type: parse.EdgeCalls},
		{From: "method:Top.swift#Top.useBar", To: "method:Foo+Ext.swift#Foo.bar", Type: parse.EdgeCalls},
		{From: "type:Leaf.swift#Leaf", To: "method:Leaf.swift#Leaf.make", Type: parse.EdgeContains},
	} {
		if err := s.PutEdge(e); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func filePaths(res rank.ImpactResult) string {
	var out []string
	for _, f := range res.Files {
		out = append(out, f.Path)
	}
	return strings.Join(out, ",")
}

func TestResolveSeedPrefersTypeDeclarationOverModuleAndExtensions(t *testing.T) {
	s := impactFixture(t)
	got, err := rank.ResolveSeed(s, "Foo")
	if err != nil {
		t.Fatalf("Foo should resolve to its declaration: %v", err)
	}
	if got != "type:Foo.swift#Foo" {
		t.Fatalf("ResolveSeed(Foo) = %s", got)
	}

	// A function with the same name keeps the lookup ambiguous.
	if err := s.PutNode(graph.Node{ID: "func:util.swift#Foo", Kind: parse.KindFunction, Name: "Foo", Path: "util.swift"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rank.ResolveSeed(s, "Foo"); err == nil {
		t.Fatal("type + function named Foo should be ambiguous")
	}
}

func TestAffectedIncludesExtensionMembersAndExpandsTypesAtEveryHop(t *testing.T) {
	s := impactFixture(t)

	// Seeding the declaration reaches callers of a method declared in an extension.
	res, err := rank.Affected(s, "type:Foo.swift#Foo", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := filePaths(res); got != "Top.swift" {
		t.Fatalf("affected by Foo = %q, want Top.swift (via extension member bar)", got)
	}

	// Depth 1: Mid references Leaf. Depth 2: Mid is a type, so callers of its
	// member Mid.run (Top.go) are affected too.
	d1, err := rank.Affected(s, "type:Leaf.swift#Leaf", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := filePaths(d1); got != "Foo+Ext.swift,Mid.swift" {
		t.Fatalf("depth 1 affected by Leaf = %q", got)
	}
	d2, err := rank.Affected(s, "type:Leaf.swift#Leaf", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := filePaths(d2); got != "Foo+Ext.swift,Mid.swift,Top.swift" {
		t.Fatalf("depth 2 affected by Leaf = %q", got)
	}
}

func TestDependenciesIncludeExtensionMembers(t *testing.T) {
	s := impactFixture(t)
	res, err := rank.Dependencies(s, "type:Foo.swift#Foo", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := filePaths(res); got != "Leaf.swift" {
		t.Fatalf("Foo dependencies = %q, want Leaf.swift (bar calls Leaf.make)", got)
	}
}

func TestImpactTruncationCountsReturnedFilesOnly(t *testing.T) {
	s := impactFixture(t)
	res, err := rank.Affected(s, "type:Leaf.swift#Leaf", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.TotalFiles != 3 || len(res.Files) != 1 {
		t.Fatalf("truncated=%v total=%d files=%d", res.Truncated, res.TotalFiles, len(res.Files))
	}
	if res.Symbols != len(res.Files[0].Symbols) {
		t.Fatalf("symbols=%d, want %d (returned files only)", res.Symbols, len(res.Files[0].Symbols))
	}
}

func TestShortestPathEdgeCases(t *testing.T) {
	s := impactFixture(t)

	// Into a type target through an extension member.
	p, err := rank.ShortestPath(s, "method:Top.swift#Top.useBar", "type:Foo.swift#Foo", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 2 || p.Steps[1].ID != "method:Foo+Ext.swift#Foo.bar" || p.Steps[1].Edge != "calls" {
		t.Fatalf("path to Foo = %+v", p.Steps)
	}

	// from == to is a zero-hop path.
	same, err := rank.ShortestPath(s, "type:Mid.swift#Mid", "type:Mid.swift#Mid", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(same.Steps) != 1 {
		t.Fatalf("from == to: %+v", same.Steps)
	}

	// From a type, a contains step into a member counts as a hop.
	viaMember, err := rank.ShortestPath(s, "type:Top.swift#Top", "type:Mid.swift#Mid", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(viaMember.Steps) != 3 || viaMember.Steps[1].Edge != "contains" || viaMember.Steps[2].Edge != "calls" {
		t.Fatalf("Top -> Mid = %+v", viaMember.Steps)
	}

	// Edges are followed in their own direction only.
	if _, err := rank.ShortestPath(s, "type:Leaf.swift#Leaf", "type:Top.swift#Top", 0); !errors.Is(err, graph.ErrNotFound) {
		t.Fatalf("Leaf -> Top should have no path, err=%v", err)
	}
}
