package index_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/index"
	"github.com/taricsa/synapse/internal/store/badger"
	"github.com/taricsa/synapse/internal/store/memory"
)

func TestIndexerSkipUnchangedAndDelete(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	writeGo(t, a, "package sample\n\nfunc Alpha() {}\n")
	writeGo(t, b, "package sample\n\nfunc Beta() {}\n")

	store := memory.New()
	idx := index.New(store)

	stats, err := idx.Run(root, index.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed != 2 || stats.Skipped != 0 || stats.Deleted != 0 {
		t.Fatalf("first run stats=%+v", stats)
	}
	if _, err := store.GetNode("func:a.go#Alpha"); err != nil {
		t.Fatalf("Alpha missing: %v", err)
	}

	stats, err = idx.Run(root, index.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed != 0 || stats.Skipped != 2 {
		t.Fatalf("second run should skip, got %+v", stats)
	}

	writeGo(t, a, "package sample\n\nfunc Alpha() {}\n\nfunc Alpha2() {}\n")
	stats, err = idx.Run(root, index.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed != 1 || stats.Skipped != 1 {
		t.Fatalf("edit run stats=%+v", stats)
	}
	if _, err := store.GetNode("func:a.go#Alpha2"); err != nil {
		t.Fatalf("Alpha2 missing: %v", err)
	}

	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	stats, err = idx.Run(root, index.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Deleted != 1 {
		t.Fatalf("delete run stats=%+v", stats)
	}
	if _, err := store.GetNode("func:b.go#Beta"); err != graph.ErrNotFound {
		t.Fatalf("Beta should be gone, err=%v", err)
	}
	if _, err := store.GetNode("file:b.go"); err != graph.ErrNotFound {
		t.Fatalf("file:b.go should be gone, err=%v", err)
	}
}

func TestIndexerBadgerEditDeleteRace(t *testing.T) {
	root := t.TempDir()
	writeGo(t, filepath.Join(root, "one.go"), "package p\n\nfunc One() {}\n")
	writeGo(t, filepath.Join(root, "two.go"), "package p\n\nfunc Two() {}\n")

	store, err := badger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	idx := index.New(store)
	if _, err := idx.Run(root, index.Options{Workers: 4}); err != nil {
		t.Fatal(err)
	}
	writeGo(t, filepath.Join(root, "one.go"), "package p\n\nfunc One() {}\n\nfunc OneB() {}\n")
	if err := os.Remove(filepath.Join(root, "two.go")); err != nil {
		t.Fatal(err)
	}
	stats, err := idx.Run(root, index.Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed < 1 || stats.Deleted != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if _, err := store.GetNode("func:one.go#OneB"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNode("func:two.go#Two"); err != graph.ErrNotFound {
		t.Fatalf("two.go nodes linger: %v", err)
	}
}

func writeGo(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
