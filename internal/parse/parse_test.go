package parse_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/parse"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures")
}

func TestRegistryRouting(t *testing.T) {
	reg := parse.NewRegistry()
	cases := map[string]string{
		"a.go":    "go",
		"b.TS":    "typescript",
		"c.tsx":   "tsx",
		"d.jsx":   "jsx",
		"e.js":    "javascript",
		"f.mjs":   "javascript",
		"g.cjs":   "javascript",
		"h.py":    "python",
		"i.SWIFT": "swift",
		"j.txt":   "",
		"noext":   "",
	}
	for path, want := range cases {
		lang := reg.Lookup(path)
		got := ""
		if lang != nil {
			got = lang.Name
		}
		if got != want {
			t.Errorf("Lookup(%q)=%q want %q", path, got, want)
		}
	}
}

func TestParseSmokeGoAndTS(t *testing.T) {
	root := fixtureRoot(t)
	reg := parse.NewRegistry()

	goPath := filepath.Join(root, "go", "sample", "greet.go")
	res, err := parse.ParseFile(reg, goPath)
	if err != nil {
		t.Fatalf("parse go: %v", err)
	}
	if res.Skipped || res.Lang != "go" {
		t.Fatalf("unexpected go result: %+v", res)
	}
	assertHasKind(t, res, parse.KindFunction, "main")
	assertHasKind(t, res, parse.KindMethod, "Greet")
	assertHasKind(t, res, parse.KindType, "Greeter")
	assertHasKind(t, res, parse.KindImport, "fmt")

	tsPath := filepath.Join(root, "ts", "sample", "greet.ts")
	res, err = parse.ParseFile(reg, tsPath)
	if err != nil {
		t.Fatalf("parse ts: %v", err)
	}
	if res.Lang != "typescript" {
		t.Fatalf("want typescript, got %q", res.Lang)
	}
	assertHasKind(t, res, parse.KindFunction, "greet")
	assertHasKind(t, res, parse.KindType, "User")
	assertHasKind(t, res, parse.KindImport, "fs")

	tsxPath := filepath.Join(root, "tsx", "sample", "app.tsx")
	res, err = parse.ParseFile(reg, tsxPath)
	if err != nil {
		t.Fatalf("parse tsx: %v", err)
	}
	if res.Lang != "tsx" {
		t.Fatalf("want tsx, got %q", res.Lang)
	}
	assertHasKind(t, res, parse.KindFunction, "Badge")
	assertHasKind(t, res, parse.KindType, "Props")
}

func TestParseSmokeJSPythonSwift(t *testing.T) {
	root := fixtureRoot(t)
	reg := parse.NewRegistry()

	jsPath := filepath.Join(root, "js", "sample", "greet.js")
	res, err := parse.ParseFile(reg, jsPath)
	if err != nil {
		t.Fatalf("parse js: %v", err)
	}
	if res.Skipped || res.Lang != "javascript" {
		t.Fatalf("unexpected js result: %+v", res)
	}
	assertHasKind(t, res, parse.KindFunction, "greet")
	assertHasKind(t, res, parse.KindFunction, "format")
	assertHasKind(t, res, parse.KindType, "Greeter")
	assertHasKind(t, res, parse.KindImport, "fs")
	assertHasKind(t, res, parse.KindImport, "fs/promises")

	jsxPath := filepath.Join(root, "jsx", "sample", "app.jsx")
	res, err = parse.ParseFile(reg, jsxPath)
	if err != nil {
		t.Fatalf("parse jsx: %v", err)
	}
	if res.Lang != "jsx" {
		t.Fatalf("want jsx, got %q", res.Lang)
	}
	assertHasKind(t, res, parse.KindFunction, "Badge")
	assertHasKind(t, res, parse.KindType, "App")
	assertHasKind(t, res, parse.KindImport, "react")

	pyPath := filepath.Join(root, "python", "sample", "greet.py")
	res, err = parse.ParseFile(reg, pyPath)
	if err != nil {
		t.Fatalf("parse python: %v", err)
	}
	if res.Lang != "python" {
		t.Fatalf("want python, got %q", res.Lang)
	}
	assertHasKind(t, res, parse.KindFunction, "helper")
	assertHasKind(t, res, parse.KindMethod, "greet")
	assertHasKind(t, res, parse.KindType, "Greeter")
	assertHasKind(t, res, parse.KindImport, "os")
	assertHasKind(t, res, parse.KindImport, "pathlib")

	swiftPath := filepath.Join(root, "swift", "sample", "greet.swift")
	res, err = parse.ParseFile(reg, swiftPath)
	if err != nil {
		t.Fatalf("parse swift: %v", err)
	}
	if res.Lang != "swift" {
		t.Fatalf("want swift, got %q", res.Lang)
	}
	assertHasKind(t, res, parse.KindFunction, "helper")
	assertHasKind(t, res, parse.KindType, "User")
	assertHasKind(t, res, parse.KindType, "Greeter")
	assertHasKind(t, res, parse.KindMethod, "greet")
	assertHasKind(t, res, parse.KindImport, "Foundation")
}

func TestGoldenFixtures(t *testing.T) {
	root := fixtureRoot(t)
	reg := parse.NewRegistry()
	cases := []struct {
		src    string
		golden string
	}{
		{filepath.Join(root, "go", "sample", "greet.go"), filepath.Join(root, "go", "sample", "greet.golden.json")},
		{filepath.Join(root, "ts", "sample", "greet.ts"), filepath.Join(root, "ts", "sample", "greet.golden.json")},
		{filepath.Join(root, "tsx", "sample", "app.tsx"), filepath.Join(root, "tsx", "sample", "app.golden.json")},
		{filepath.Join(root, "js", "sample", "greet.js"), filepath.Join(root, "js", "sample", "greet.golden.json")},
		{filepath.Join(root, "jsx", "sample", "app.jsx"), filepath.Join(root, "jsx", "sample", "app.golden.json")},
		{filepath.Join(root, "python", "sample", "greet.py"), filepath.Join(root, "python", "sample", "greet.golden.json")},
		{filepath.Join(root, "swift", "sample", "greet.swift"), filepath.Join(root, "swift", "sample", "greet.golden.json")},
	}

	for _, tc := range cases {
		t.Run(filepath.Base(tc.src), func(t *testing.T) {
			res, err := parse.ParseFile(reg, tc.src)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(root, tc.src)
			if err != nil {
				t.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			stabilizePaths(&res, filepath.ToSlash(tc.src), rel)

			got, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')

			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(tc.golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("read golden (run UPDATE_GOLDEN=1): %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tc.src, got, want)
			}
		})
	}
}

func TestWalkSkipsIgnoredDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ok.go"), "package ok\n")
	mustWrite(t, filepath.Join(root, "vendor", "x.go"), "package x\n")
	mustWrite(t, filepath.Join(root, "node_modules", "y.ts"), "export const y = 1\n")
	mustWrite(t, filepath.Join(root, ".git", "z.go"), "package z\n")
	mustWrite(t, filepath.Join(root, "keep", "a.ts"), "export const a = 1\n")

	files, err := parse.ListSourceFiles(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f, "vendor") || strings.Contains(f, "node_modules") || strings.Contains(f, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			t.Fatalf("ignored path leaked: %s", f)
		}
	}
	if len(files) != 2 {
		t.Fatalf("want 2 source files, got %d: %v", len(files), files)
	}

	wr, err := parse.WalkTree(root, parse.WalkOptions{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(wr.Errors) != 0 {
		t.Fatalf("errors: %v", wr.Errors)
	}
	if len(wr.Results) != 2 {
		t.Fatalf("want 2 results, got %d", len(wr.Results))
	}
}

func TestWalkRace(t *testing.T) {
	root := fixtureRoot(t)
	wr, err := parse.WalkTree(root, parse.WalkOptions{Workers: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(wr.Errors) != 0 {
		t.Fatalf("errors: %v", wr.Errors)
	}
	if len(wr.Results) < 3 {
		t.Fatalf("want >=3 results, got %d", len(wr.Results))
	}
}

func TestTypeScriptMethodIDsIncludeClassName(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "classes.ts")
	mustWrite(t, path, `
export class Alpha {
  render() {
    console.log("alpha")
  }
}

export class Beta {
  render() {
    console.log("beta")
  }
}
`)

	res, err := parse.ParseFile(parse.NewRegistry(), path)
	if err != nil {
		t.Fatal(err)
	}

	var methods []graph.NodeID
	for _, n := range res.Nodes {
		if n.Kind == parse.KindMethod && n.Name == "render" {
			methods = append(methods, n.ID)
		}
	}
	if len(methods) != 2 {
		t.Fatalf("want two render methods, got %d: %v", len(methods), methods)
	}
	if methods[0] == methods[1] {
		t.Fatalf("method IDs collided: %v", methods)
	}
	for _, want := range []string{"#Alpha.render", "#Beta.render"} {
		found := false
		for _, id := range methods {
			if strings.Contains(string(id), want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing method ID containing %q in %v", want, methods)
		}
	}
}

func TestNestedPythonFunctionNotMethod(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested.py")
	mustWrite(t, path, `
class Greeter:
    def greet(self):
        def helper():
            print("hi")
        helper()
`)
	res, err := parse.ParseFile(parse.NewRegistry(), path)
	if err != nil {
		t.Fatal(err)
	}
	assertHasKind(t, res, parse.KindMethod, "greet")
	assertHasKind(t, res, parse.KindFunction, "helper")
	for _, n := range res.Nodes {
		if n.Name == "helper" && n.Kind == parse.KindMethod {
			t.Fatalf("nested helper should be function, got method %s", n.ID)
		}
	}
}

func TestNestedSwiftFunctionNotMethod(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested.swift")
	mustWrite(t, path, `
class Greeter {
    func greet() {
        func helper() {
            print("hi")
        }
        helper()
    }
}
`)
	res, err := parse.ParseFile(parse.NewRegistry(), path)
	if err != nil {
		t.Fatal(err)
	}
	assertHasKind(t, res, parse.KindMethod, "greet")
	assertHasKind(t, res, parse.KindFunction, "helper")
	for _, n := range res.Nodes {
		if n.Name == "helper" && n.Kind == parse.KindMethod {
			t.Fatalf("nested helper should be function, got method %s", n.ID)
		}
	}
}

func TestJSDefaultParamCallsCaptured(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "defaults.js")
	mustWrite(t, path, `
const foo = (x = bar()) => {
  return x;
};
`)
	res, err := parse.ParseFile(parse.NewRegistry(), path)
	if err != nil {
		t.Fatal(err)
	}
	assertHasKind(t, res, parse.KindFunction, "foo")
	assertHasKind(t, res, parse.KindSymbol, "bar")
	found := false
	for _, e := range res.Edges {
		if e.Type == parse.EdgeCalls && strings.Contains(string(e.From), "#foo") && string(e.To) == "symbol:bar" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("want foo -> symbol:bar calls edge, got %+v", res.Edges)
	}
}

func TestNormalizeDeduplicatesEdges(t *testing.T) {
	res := parse.Result{
		Edges: []graph.Edge{
			{From: "b", To: "c", Type: "calls"},
			{From: "a", To: "b", Type: "contains"},
			{From: "a", To: "b", Type: "contains"},
			{From: "b", To: "c", Type: "calls"},
		},
	}
	res.Normalize()
	if len(res.Edges) != 2 {
		t.Fatalf("want 2 unique edges, got %d: %+v", len(res.Edges), res.Edges)
	}
}

func TestWalkTreeRejectsMissingRoot(t *testing.T) {
	_, err := parse.WalkTree(filepath.Join(t.TempDir(), "missing"), parse.WalkOptions{})
	if err == nil {
		t.Fatal("expected missing root error")
	}
}

func assertHasKind(t *testing.T, res parse.Result, kind, name string) {
	t.Helper()
	for _, n := range res.Nodes {
		if n.Kind == kind && n.Name == name {
			return
		}
	}
	t.Fatalf("missing node kind=%s name=%s in %+v", kind, name, res.Nodes)
}

func stabilizePaths(res *parse.Result, abs, rel string) {
	res.Path = rel
	replace := func(s string) string {
		s = strings.ReplaceAll(s, abs, rel)
		return s
	}
	for i := range res.Nodes {
		n := &res.Nodes[i]
		n.ID = graph.NodeID(replace(string(n.ID)))
		n.Path = replace(n.Path)
		if n.Kind == parse.KindFile {
			n.Name = rel
		}
	}
	for i := range res.Edges {
		e := &res.Edges[i]
		e.From = graph.NodeID(replace(string(e.From)))
		e.To = graph.NodeID(replace(string(e.To)))
	}
	res.Normalize()
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
