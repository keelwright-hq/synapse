package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/memory"
	"github.com/keelwright-hq/synapse/internal/uri"
)

func TestIndexerAssignsRepoURI(t *testing.T) {
	root := t.TempDir()
	writeGo(t, filepath.Join(root, "a.go"), "package sample\n\nfunc Alpha() {}\n")

	store := memory.New()
	idx := index.New(store)
	if _, err := idx.Run(root, index.Options{Workers: 1, Repo: "synapse"}); err != nil {
		t.Fatal(err)
	}

	n, err := store.GetNode("func:a.go#Alpha")
	if err != nil {
		t.Fatal(err)
	}
	want := "repo://synapse/a.go#func:Alpha"
	if n.Props[uri.PropKey] != want {
		t.Fatalf("props=%v want %s", n.Props, want)
	}
	got, err := store.GetNodeByURI(want)
	if err != nil || got.ID != n.ID {
		t.Fatalf("uri index: %+v %v", got, err)
	}

	id, err := rank.ResolveSeed(store, want)
	if err != nil || id != n.ID {
		t.Fatalf("ResolveSeed uri: %v %v", id, err)
	}
	id, err = rank.ResolveSeed(store, string(n.ID))
	if err != nil || id != n.ID {
		t.Fatalf("ResolveSeed legacy: %v %v", id, err)
	}
}

func TestCrossRepoSamePathDifferentURI(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "svc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGo(t, filepath.Join(dir, "handler.go"), "package svc\n\nfunc Handle() {}\n")

	s1 := memory.New()
	s2 := memory.New()
	idx1 := index.New(s1)
	idx2 := index.New(s2)
	if _, err := idx1.Run(root, index.Options{Repo: "api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := idx2.Run(root, index.Options{Repo: "worker"}); err != nil {
		t.Fatal(err)
	}

	u1 := "repo://api/svc/handler.go#func:Handle"
	u2 := "repo://worker/svc/handler.go#func:Handle"
	if _, err := s1.GetNodeByURI(u1); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.GetNodeByURI(u2); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.GetNodeByURI(u2); err == nil {
		t.Fatal("api store should not resolve worker uri")
	}
}
