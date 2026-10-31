package index_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/store/badger"
	"github.com/keelwright-hq/synapse/internal/store/memory"
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

func TestIndexerProcessesJavaScript(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte(`function hello() { console.log("hi") }`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := memory.New()
	stats, err := index.New(store).Run(root, index.Options{Workers: 1, Repo: "jsdemo"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Processed < 1 {
		t.Fatalf("want processed>=1, got %+v", stats)
	}
	if _, err := store.GetNode("func:app.js#hello"); err != nil {
		t.Fatalf("hello missing: %v", err)
	}
}

func TestIndexerResolvesCallsAndIncrementalUpdates(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	writeGo(t, a, "package sample\n\nfunc Caller() {\n\tHelper()\n\tExternalFn()\n}\n")
	writeGo(t, b, "package sample\n\nfunc Helper() {}\n")

	store := memory.New()
	idx := index.New(store)
	if _, err := idx.Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}

	inEdges, err := store.InEdges("func:b.go#Helper", "calls")
	if err != nil {
		t.Fatal(err)
	}
	if len(inEdges) != 1 || inEdges[0].From != "func:a.go#Caller" {
		t.Fatalf("expected Caller -> func:b.go#Helper calls edge, got %+v", inEdges)
	}
	if _, err := store.GetNode("symbol:Helper"); err != graph.ErrNotFound {
		t.Fatalf("expected resolved symbol:Helper placeholder to be pruned, got err=%v", err)
	}
	if _, err := store.GetNode("symbol:ExternalFn"); err != nil {
		t.Fatalf("expected unresolved symbol:ExternalFn to remain, got err=%v", err)
	}

	// Delete b.go and re-run incrementally (a.go is skipped); symbol:Helper should be restored.
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	stats, err := idx.Run(root, index.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 1 || stats.Deleted != 1 {
		t.Fatalf("unexpected incremental stats=%+v", stats)
	}
	symEdges, err := store.InEdges("symbol:Helper", "calls")
	if err != nil {
		t.Fatal(err)
	}
	if len(symEdges) != 1 || symEdges[0].From != "func:a.go#Caller" {
		t.Fatalf("expected Caller -> symbol:Helper to be restored after deleting b.go, got %+v", symEdges)
	}
}

func TestIndexerQualifiedCallDoesNotBindLocalDefinition(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "util"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeGo(t, filepath.Join(root, "a.go"), `package main

import (
	"fmt"
	"example.com/demo/util"
)

func Println(s string) {}

func main() {
	fmt.Println("hi")
	fmt.Printf("hi")
	fmt.Sprintln("hi")
	util.Helper()
}
`)
	writeGo(t, filepath.Join(root, "b.go"), "package main\n\nfunc Printf(s string) {}\n")
	writeGo(t, filepath.Join(root, "sub", "c.go"), "package sub\n\nfunc Sprintln(s string) {}\n")
	writeGo(t, filepath.Join(root, "util", "helper.go"), "package util\n\nfunc Helper() {}\n")

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}

	for _, localDef := range []graph.NodeID{
		"func:a.go#Println",
		"func:b.go#Printf",
		"func:sub/c.go#Sprintln",
	} {
		inEdges, err := store.InEdges(localDef, "calls")
		if err != nil {
			t.Fatal(err)
		}
		if len(inEdges) != 0 {
			t.Fatalf("expected no calls to %s from fmt.*, got %+v", localDef, inEdges)
		}
	}

	for _, unresolvedSym := range []graph.NodeID{
		"symbol:fmt.Println",
		"symbol:fmt.Printf",
		"symbol:fmt.Sprintln",
	} {
		inEdges, err := store.InEdges(unresolvedSym, "calls")
		if err != nil {
			t.Fatal(err)
		}
		if len(inEdges) != 1 || inEdges[0].From != "func:a.go#main" {
			t.Fatalf("expected main -> %s to remain unresolved, got %+v", unresolvedSym, inEdges)
		}
	}

	utilEdges, err := store.InEdges("func:util/helper.go#Helper", "calls")
	if err != nil {
		t.Fatal(err)
	}
	if len(utilEdges) != 1 || utilEdges[0].From != "func:a.go#main" {
		t.Fatalf("expected main -> func:util/helper.go#Helper via imported package, got %+v", utilEdges)
	}
}

func TestIndexerImportDoesNotMatchRepoFilename(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "util"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeGo(t, filepath.Join(root, "main.go"), `package main

import "fmt"

func main() {
	Sprintf("hi")
	fmt.Sprintf("hi")
}
`)
	writeGo(t, filepath.Join(root, "util", "fmt.go"), "package util\n\nfunc Sprintf(s string) {}\n")
	writeGo(t, filepath.Join(root, "other", "str.go"), "package other\n\nfunc Sprintf(s string) {}\n")

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}

	inEdges, err := store.InEdges("func:util/fmt.go#Sprintf", "calls")
	if err != nil {
		t.Fatal(err)
	}
	if len(inEdges) != 0 {
		t.Fatalf("expected import \"fmt\" not to select util/fmt.go, got incoming calls %+v", inEdges)
	}

	for _, sym := range []graph.NodeID{"symbol:Sprintf", "symbol:fmt.Sprintf"} {
		symEdges, err := store.InEdges(sym, "calls")
		if err != nil {
			t.Fatal(err)
		}
		if len(symEdges) != 1 || symEdges[0].From != "func:main.go#main" {
			t.Fatalf("expected %s to remain unresolved, got %+v", sym, symEdges)
		}
	}
}

func TestIndexerFileNodeNameIsRelativeBasenameAndRespectsGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "third_party"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGo(t, filepath.Join(root, ".gitignore"), "ignored/\n")
	writeGo(t, filepath.Join(root, "sub", "hello.go"), "package sub\n\nfunc Hello() { println(\"hi\") }\n")
	writeGo(t, filepath.Join(root, "third_party", "vendored.go"), "package tp\n\nfunc Vendored() {}\n")
	writeGo(t, filepath.Join(root, "ignored", "skip.go"), "package ig\n\nfunc Skip() {}\n")

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}

	fn, err := store.GetNode("file:sub/hello.go")
	if err != nil {
		t.Fatalf("missing file:sub/hello.go: %v", err)
	}
	if fn.Name != "hello.go" {
		t.Fatalf("file node Name=%q, want %q", fn.Name, "hello.go")
	}
	if strings.Contains(fn.Name, root) || strings.Contains(fn.Path, root) {
		t.Fatalf("file node leaked absolute root path: %+v", fn)
	}
	sym, err := store.GetNode("symbol:println")
	if err != nil {
		t.Fatalf("missing symbol:println: %v", err)
	}
	if sym.Path != "" {
		t.Fatalf("expected shared symbol node to have empty Path, got %q", sym.Path)
	}
	if _, err := store.GetNode("file:third_party/vendored.go"); err != graph.ErrNotFound {
		t.Fatalf("expected third_party file to be ignored, got err=%v", err)
	}
	if _, err := store.GetNode("file:ignored/skip.go"); err != graph.ErrNotFound {
		t.Fatalf("expected gitignored file to be ignored, got err=%v", err)
	}
}

func TestIndexerResolvesSwiftCallsAcrossFiles(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"App", "Services", "Models"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeGo(t, filepath.Join(root, "Models", "Temperature.swift"), `struct Temperature {
    let value: Double
}
`)
	writeGo(t, filepath.Join(root, "Services", "NotificationService.swift"), `final class NotificationService {
    static let shared = NotificationService()
    func reconcile() {}
}
`)
	writeGo(t, filepath.Join(root, "Services", "Store.swift"), `final class Store {
    func refreshAll() {}
}
`)
	writeGo(t, filepath.Join(root, "App", "ViewModel.swift"), `final class ViewModel {
    let store = Store()
    func load() {
        let t = Temperature(value: 1)
        NotificationService.shared.reconcile()
        store.refreshAll()
        JSONDecoder().decodeThing()
        Date()
        _ = Double(2)
    }
}
`)
	writeGo(t, filepath.Join(root, "App", "Other.swift"), `final class Other {
    func decodeThing() {}
}
`)
	// Extensions are not definitions: Temperature keeps one constructible type,
	// and an extension of an external type does not capture Double(...) calls.
	writeGo(t, filepath.Join(root, "Models", "Temperature+Format.swift"), `extension Temperature {
    func label() -> String { "" }
}
extension Double {
    func roundedHalf() -> Double { self }
}
`)

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}
	caller := graph.NodeID("method:App/ViewModel.swift#ViewModel.load")
	out, err := store.OutEdges(caller, "calls")
	if err != nil {
		t.Fatal(err)
	}
	got := map[graph.NodeID]bool{}
	for _, e := range out {
		got[e.To] = true
	}
	for _, want := range []graph.NodeID{
		"type:Models/Temperature.swift#Temperature",
		"method:Services/NotificationService.swift#NotificationService.reconcile",
		"method:Services/Store.swift#Store.refreshAll",
		"symbol:JSONDecoder.decodeThing",
		"symbol:Date",
		"symbol:Double",
	} {
		if !got[want] {
			t.Errorf("missing calls edge %s -> %s (got %v)", caller, want, got)
		}
	}
	if got["method:App/Other.swift#Other.decodeThing"] {
		t.Errorf("call on external type JSONDecoder bound to a repo method")
	}
}
