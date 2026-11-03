package index

import (
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// resolveImports links each module's local imports to the imported file's
// module with an imports edge (module -> module), next to the unresolved
// import:<spec> placeholder the parser records. It covers relative JS/TS
// specifiers (./x, ../x, ./x.js for x.ts, directory index files) and Python
// relative and repo-local absolute imports (including a src/ layout).
// Package imports (react, fastapi) and path aliases are left unresolved.
func (idx *Indexer) resolveImports(log *slog.Logger) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	files := map[string]bool{}
	modules := map[graph.NodeID]bool{}
	var imports []graph.Node
	err := idx.store.ForEachNode(func(n graph.Node) bool {
		switch n.Kind {
		case parse.KindFile:
			if n.Path != "" {
				files[n.Path] = true
			}
		case parse.KindModule:
			modules[n.ID] = true
		case parse.KindImport:
			if n.Path != "" && n.Name != "" {
				imports = append(imports, n)
			}
		}
		return true
	})
	if err != nil {
		return err
	}

	desired := map[graph.NodeID]map[graph.NodeID]string{}
	for _, imp := range imports {
		target, ok := resolveImportPath(imp.Path, imp.Name, files)
		if !ok || target == imp.Path {
			continue
		}
		from, to := graph.NodeID("module:"+imp.Path), graph.NodeID("module:"+target)
		if !modules[from] || !modules[to] {
			continue
		}
		if desired[from] == nil {
			desired[from] = map[graph.NodeID]string{}
		}
		desired[from][to] = imp.Name
	}

	ids := make([]graph.NodeID, 0, len(modules))
	for id := range modules {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	linked := 0
	for _, from := range ids {
		existing, err := idx.store.OutEdges(from, parse.EdgeImports)
		if err != nil {
			return err
		}
		for _, e := range existing {
			// Only module->module edges are ours; import placeholders stay.
			if strings.HasPrefix(string(e.To), "module:") {
				if _, keep := desired[from][e.To]; !keep {
					if err := idx.store.DeleteEdge(e.From, e.To, e.Type); err != nil && err != graph.ErrNotFound {
						return err
					}
				}
			}
		}
		targets := make([]graph.NodeID, 0, len(desired[from]))
		for to := range desired[from] {
			targets = append(targets, to)
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
		for _, to := range targets {
			if err := idx.store.PutEdge(graph.Edge{
				From: from, To: to, Type: parse.EdgeImports,
				Props: map[string]string{parse.PropProvenance: parse.ProvenanceExtracted, "spec": desired[from][to]},
			}); err != nil {
				return err
			}
			linked++
		}
	}
	log.Info("index resolved imports", "count", linked)
	return nil
}

// resolveImportPath maps an import specifier written in importer to an indexed
// repo-relative file path.
func resolveImportPath(importer, spec string, files map[string]bool) (string, bool) {
	dir := path.Dir(importer)
	switch path.Ext(importer) {
	case ".py":
		return resolvePythonImport(dir, spec, files)
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") && spec != "." && spec != ".." {
			return "", false
		}
		base := path.Clean(path.Join(dir, spec))
		cands := []string{base}
		// TS ESM imports name the emitted file: ./http.js -> ./http.ts,
		// ./x.mjs -> ./x.mts, ./x.cjs -> ./x.cts.
		if ext := path.Ext(base); ext != "" {
			stem := strings.TrimSuffix(base, ext)
			emitted := map[string][]string{".js": {".ts", ".tsx"}, ".jsx": {".tsx"}, ".mjs": {".mts"}, ".cjs": {".cts"}}
			for _, src := range emitted[ext] {
				cands = append(cands, stem+src)
			}
		}
		for _, ext := range []string{".js", ".ts", ".tsx", ".jsx", ".mjs", ".cjs", ".mts", ".cts"} {
			cands = append(cands, base+ext)
		}
		for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
			cands = append(cands, base+"/index"+ext)
		}
		for _, c := range cands {
			if files[c] {
				return c, true
			}
		}
	}
	return "", false
}

func resolvePythonImport(dir, spec string, files map[string]bool) (string, bool) {
	var bases []string
	if strings.HasPrefix(spec, ".") {
		dots := len(spec) - len(strings.TrimLeft(spec, "."))
		d := dir
		for i := 1; i < dots; i++ {
			d = path.Dir(d)
		}
		rest := strings.TrimLeft(spec, ".")
		if rest == "" {
			bases = append(bases, d)
		} else {
			bases = append(bases, path.Join(d, strings.ReplaceAll(rest, ".", "/")))
		}
	} else {
		// An absolute import is relative to the project's package root, which
		// may be nested (svc/src/app/x.py imports `app.models`): try each
		// ancestor of the importing file, closest first, then a src/ layout.
		mod := strings.ReplaceAll(spec, ".", "/")
		for d := dir; ; d = path.Dir(d) {
			if d == "." || d == "/" || d == "" {
				bases = append(bases, mod, path.Join("src", mod))
				break
			}
			bases = append(bases, path.Join(d, mod))
		}
	}
	for _, b := range bases {
		for _, c := range []string{b + ".py", b + "/__init__.py"} {
			if files[c] {
				return c, true
			}
		}
	}
	return "", false
}
