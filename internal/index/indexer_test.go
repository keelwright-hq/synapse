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
	for _, dir := range []string{"App", "Services", "Models", "Extensions"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeGo(t, filepath.Join(root, "Models", "Temperature.swift"), `struct Temperature {
    let value: Double
    func summary() -> String {
        _ = self.rounded()
        return label()
    }
}
`)
	writeGo(t, filepath.Join(root, "Models", "Wind.swift"), `struct Wind {
    func label() -> String { "" }
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
    func refreshAll() {
        self.store.refreshAll()
    }
    func load() {
        let t = Temperature(value: 1)
        NotificationService.shared.reconcile()
        self.store.refreshAll()
        JSONDecoder().decodeThing()
        self.decodeThing()
        Temperature.decodeThing()
        "literal".decodeThing()
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
	// Putting Temperature+Format in Extensions/ also verifies that Temperature.summary's
	// bare label() call resolves to Temperature.label across directories rather than
	// same-directory Wind.label in Models/.
	writeGo(t, filepath.Join(root, "Extensions", "Temperature+Format.swift"), `extension Temperature {
    func label() -> String { "" }
    func rounded() -> Temperature { self }
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
		"symbol:self.decodeThing",
		"symbol:Temperature.decodeThing",
		"symbol:_.decodeThing",
		"symbol:Date",
		"symbol:Double",
	} {
		if !got[want] {
			t.Errorf("missing calls edge %s -> %s (got %v)", caller, want, got)
		}
	}
	if got["method:App/Other.swift#Other.decodeThing"] {
		t.Errorf("call on external/unrelated receiver bound to Other.decodeThing")
	}
	if got["method:App/ViewModel.swift#ViewModel.refreshAll"] {
		t.Errorf("self.store.refreshAll() falsely bound to ViewModel.refreshAll instead of Store.refreshAll")
	}

	wrapperCaller := graph.NodeID("method:App/ViewModel.swift#ViewModel.refreshAll")
	wrapperOut, err := store.OutEdges(wrapperCaller, "calls")
	if err != nil {
		t.Fatal(err)
	}
	if len(wrapperOut) != 1 || wrapperOut[0].To != "method:Services/Store.swift#Store.refreshAll" {
		t.Errorf("expected ViewModel.refreshAll -> Store.refreshAll, got %+v", wrapperOut)
	}

	tempSummary := graph.NodeID("method:Models/Temperature.swift#Temperature.summary")
	tempOut, err := store.OutEdges(tempSummary, "calls")
	if err != nil {
		t.Fatal(err)
	}
	tempGot := map[graph.NodeID]bool{}
	for _, e := range tempOut {
		tempGot[e.To] = true
	}
	for _, want := range []graph.NodeID{
		"method:Extensions/Temperature+Format.swift#Temperature.label",
		"method:Extensions/Temperature+Format.swift#Temperature.rounded",
	} {
		if !tempGot[want] {
			t.Errorf("expected Temperature.summary -> %s in extension, got %+v", want, tempOut)
		}
	}
}

func TestIndexerBareCallInMethodPrefersFunctionWithoutImplicitSelf(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "srv"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Python and Go never resolve a bare call inside a method to self.method.
	writeGo(t, filepath.Join(root, "app.py"), `def validate():
    pass

class Server:
    def validate(self):
        pass

    def start(self):
        validate()
`)
	writeGo(t, filepath.Join(root, "srv", "server.go"), "package srv\n\ntype Server struct{}\n\nfunc (s *Server) Start() { validate() }\n")
	writeGo(t, filepath.Join(root, "srv", "server_validate.go"), "package srv\n\nfunc (s *Server) validate() {}\n")
	writeGo(t, filepath.Join(root, "srv", "util.go"), "package srv\n\nfunc validate() {}\n")

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}
	for caller, want := range map[graph.NodeID]graph.NodeID{
		"method:app.py#Server.start":        "func:app.py#validate",
		"method:srv/server.go#Server.Start": "func:srv/util.go#validate",
	} {
		out, err := store.OutEdges(caller, "calls")
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 || out[0].To != want {
			t.Errorf("%s: want calls -> %s, got %+v", caller, want, out)
		}
	}
}

func TestIndexerSwiftConformerDoesNotBindToSiblingImplementation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	// center.add is UNUserNotificationCenter's API; the only other repo `add`
	// is a test fake conforming to the same protocol and must not be chosen.
	writeGo(t, filepath.Join(root, "NotificationService.swift"), `protocol NotificationClient {
    func add(_ request: Int) async throws
}
struct SystemNotificationClient: NotificationClient {
    let center = UNUserNotificationCenter.current()
    func add(_ request: Int) async throws { try await center.add(request) }
}
`)
	writeGo(t, filepath.Join(root, "Tests", "FakeClient.swift"), `final class FakeNotificationClient: NotificationClient {
    func add(_ request: Int) async throws {}
}
`)
	// A property named after its type's suffix still resolves among same-named methods.
	writeGo(t, filepath.Join(root, "WeatherRepository.swift"), `final class WeatherRepository {
    func setProvider(_ p: Int) {}
}
`)
	writeGo(t, filepath.Join(root, "WeatherViewModel.swift"), `final class WeatherViewModel {
    let repository = WeatherRepository()
    func setProvider(_ p: Int) { repository.setProvider(p) }
}
`)

	store := memory.New()
	if _, err := index.New(store).Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}
	out, err := store.OutEdges("method:NotificationService.swift#SystemNotificationClient.add", "calls")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range out {
		if e.To == "method:Tests/FakeClient.swift#FakeNotificationClient.add" {
			t.Fatalf("production add bound to test fake: %+v", out)
		}
	}

	vm, err := store.OutEdges("method:WeatherViewModel.swift#WeatherViewModel.setProvider", "calls")
	if err != nil {
		t.Fatal(err)
	}
	if len(vm) != 1 || vm[0].To != "method:WeatherRepository.swift#WeatherRepository.setProvider" {
		t.Fatalf("repository.setProvider: want WeatherRepository.setProvider, got %+v", vm)
	}
}
