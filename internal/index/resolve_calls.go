package index

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// propCallees stores comma-separated raw AST callee names on a caller node so
// incremental index runs can re-resolve calls even when the caller's file was
// skipped as unchanged.
const propCallees = "callees"

// recordCallees attaches raw AST symbol:<Name> call targets to their caller
// nodes inside res before file persistence.
func recordCallees(res parse.Result) parse.Result {
	byFrom := make(map[graph.NodeID][]string)
	for _, e := range res.Edges {
		if e.Type != parse.EdgeCalls {
			continue
		}
		to := string(e.To)
		if !strings.HasPrefix(to, "symbol:") {
			continue
		}
		name := strings.TrimPrefix(to, "symbol:")
		if name == "" {
			continue
		}
		byFrom[e.From] = append(byFrom[e.From], name)
	}
	if len(byFrom) == 0 {
		return res
	}
	for i := range res.Nodes {
		n := &res.Nodes[i]
		names := byFrom[n.ID]
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		dedup := names[:0]
		for _, s := range names {
			if len(dedup) == 0 || dedup[len(dedup)-1] != s {
				dedup = append(dedup, s)
			}
		}
		if n.Props == nil {
			n.Props = map[string]string{}
		}
		n.Props[propCallees] = strings.Join(dedup, ",")
	}
	return res
}

// resolveCalls links unresolved symbol:<Name> call edges to concrete function
// or method definitions across the indexed repository and prunes orphan symbol
// placeholders.
func (idx *Indexer) resolveCalls(log *slog.Logger) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	defsByName := make(map[string][]graph.Node)
	importsByFile := make(map[string][]string)
	var callers []graph.Node
	existingSymbols := make(map[graph.NodeID]bool)

	err := idx.store.ForEachNode(func(n graph.Node) bool {
		switch n.Kind {
		case parse.KindFunction, parse.KindMethod:
			if n.Name != "" {
				defsByName[n.Name] = append(defsByName[n.Name], n)
			}
		case parse.KindImport:
			if n.Path != "" && n.Name != "" {
				importsByFile[n.Path] = append(importsByFile[n.Path], n.Name)
			}
		case parse.KindSymbol:
			existingSymbols[n.ID] = true
		}
		if n.Props != nil && n.Props[propCallees] != "" {
			callers = append(callers, n)
		}
		return true
	})
	if err != nil {
		return err
	}

	for name := range defsByName {
		defs := defsByName[name]
		sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
		defsByName[name] = defs
	}
	sort.Slice(callers, func(i, j int) bool { return callers[i].ID < callers[j].ID })

	for _, caller := range callers {
		raw := caller.Props[propCallees]
		if raw == "" {
			continue
		}
		calleeNames := strings.Split(raw, ",")
		desired := make(map[graph.NodeID]string, len(calleeNames))
		fileImports := importsByFile[caller.Path]

		for _, calleeName := range calleeNames {
			calleeName = strings.TrimSpace(calleeName)
			if calleeName == "" {
				continue
			}
			_, memberName := splitCalleeQualifier(calleeName)
			if targetID, prov, ok := pickCalleeTarget(caller, calleeName, defsByName[memberName], fileImports); ok {
				if targetID != caller.ID {
					desired[targetID] = prov
				}
				continue
			}
			symID := graph.NodeID("symbol:" + calleeName)
			if !existingSymbols[symID] {
				if err := idx.store.PutNode(graph.Node{
					ID:   symID,
					Kind: parse.KindSymbol,
					Name: calleeName,
				}); err != nil {
					return err
				}
				existingSymbols[symID] = true
			}
			desired[symID] = parse.ProvenanceExtracted
		}

		outEdges, err := idx.store.OutEdges(caller.ID, parse.EdgeCalls)
		if err != nil {
			return err
		}
		for _, e := range outEdges {
			if _, keep := desired[e.To]; !keep {
				if err := idx.store.DeleteEdge(caller.ID, e.To, parse.EdgeCalls); err != nil {
					return err
				}
			}
		}

		targetIDs := make([]graph.NodeID, 0, len(desired))
		for id := range desired {
			targetIDs = append(targetIDs, id)
		}
		sort.Slice(targetIDs, func(i, j int) bool { return targetIDs[i] < targetIDs[j] })
		for _, to := range targetIDs {
			prov := desired[to]
			if err := idx.store.PutEdge(graph.Edge{
				From: caller.ID,
				To:   to,
				Type: parse.EdgeCalls,
				Props: map[string]string{
					parse.PropProvenance: prov,
				},
			}); err != nil {
				return err
			}
		}
	}

	// Prune orphan symbol:<Name> nodes that no longer have any incident edges.
	symIDs := make([]graph.NodeID, 0, len(existingSymbols))
	for id := range existingSymbols {
		symIDs = append(symIDs, id)
	}
	sort.Slice(symIDs, func(i, j int) bool { return symIDs[i] < symIDs[j] })
	for _, symID := range symIDs {
		inEdges, err := idx.store.InEdges(symID, "")
		if err != nil {
			return err
		}
		if len(inEdges) > 0 {
			continue
		}
		outEdges, err := idx.store.OutEdges(symID, "")
		if err != nil {
			return err
		}
		if len(outEdges) > 0 {
			continue
		}
		if err := idx.store.DeleteNode(symID); err != nil && err != graph.ErrNotFound {
			return err
		}
	}
	return nil
}

func splitCalleeQualifier(calleeName string) (qualifier, member string) {
	if i := strings.LastIndex(calleeName, "::"); i >= 0 {
		return calleeName[:i], calleeName[i+2:]
	}
	if i := strings.LastIndexByte(calleeName, '.'); i >= 0 {
		return calleeName[:i], calleeName[i+1:]
	}
	return "", calleeName
}

func pickCalleeTarget(caller graph.Node, calleeName string, candidates []graph.Node, fileImports []string) (graph.NodeID, string, bool) {
	if len(candidates) == 0 {
		return "", "", false
	}

	qualifier, _ := splitCalleeQualifier(calleeName)
	isBare := qualifier == ""

	if isBare {
		// Tier 1: Same file.
		var sameFile []graph.Node
		for _, c := range candidates {
			if c.Path == caller.Path {
				sameFile = append(sameFile, c)
			}
		}
		if len(sameFile) == 1 {
			return sameFile[0].ID, parse.ProvenanceExtracted, true
		}
		if len(sameFile) > 1 {
			if id, ok := disambiguateCandidates(caller, sameFile); ok {
				return id, parse.ProvenanceExtracted, true
			}
			return "", "", false
		}

		// Tier 2: Same directory / package.
		callerDir := filepath.ToSlash(filepath.Dir(caller.Path))
		var sameDir []graph.Node
		for _, c := range candidates {
			if filepath.ToSlash(filepath.Dir(c.Path)) == callerDir {
				sameDir = append(sameDir, c)
			}
		}
		if len(sameDir) == 1 {
			return sameDir[0].ID, parse.ProvenanceInferred, true
		}
		if len(sameDir) > 1 {
			if id, ok := disambiguateCandidates(caller, sameDir); ok {
				return id, parse.ProvenanceInferred, true
			}
			return "", "", false
		}
	}

	// Tier 3: Imported package / module.
	if len(fileImports) > 0 {
		var imported []graph.Node
		for _, c := range candidates {
			if matchesImport(caller.Path, c.Path, qualifier, fileImports) {
				imported = append(imported, c)
			}
		}
		if len(imported) == 1 {
			return imported[0].ID, parse.ProvenanceInferred, true
		}
		if len(imported) > 1 {
			if id, ok := disambiguateCandidates(caller, imported); ok {
				return id, parse.ProvenanceInferred, true
			}
			return "", "", false
		}
	}

	// Tier 4: Unique top-level function across the repository (excluding common language builtins).
	if isBare && !isCommonBuiltin(calleeName) && len(candidates) == 1 && candidates[0].Kind == parse.KindFunction {
		return candidates[0].ID, parse.ProvenanceInferred, true
	}

	return "", "", false
}

func disambiguateCandidates(caller graph.Node, candidates []graph.Node) (graph.NodeID, bool) {
	if caller.Props != nil {
		if recv := caller.Props["receiver"]; recv != "" {
			var byRecv []graph.Node
			for _, c := range candidates {
				if c.Props != nil && c.Props["receiver"] == recv {
					byRecv = append(byRecv, c)
				}
			}
			if len(byRecv) == 1 {
				return byRecv[0].ID, true
			}
		}
	}
	var funcs []graph.Node
	for _, c := range candidates {
		if c.Kind == parse.KindFunction {
			funcs = append(funcs, c)
		}
	}
	if len(funcs) == 1 {
		return funcs[0].ID, true
	}
	return "", false
}

func matchesImport(callerPath, defPath, qualifier string, fileImports []string) bool {
	callerDir := filepath.ToSlash(filepath.Dir(callerPath))
	defDir := filepath.ToSlash(filepath.Dir(defPath))
	defNoExt := strings.TrimSuffix(filepath.ToSlash(defPath), filepath.Ext(defPath))
	slashQual := strings.ReplaceAll(strings.ReplaceAll(qualifier, "::", "/"), ".", "/")

	for _, rawImp := range fileImports {
		imp := strings.Trim(strings.TrimSpace(rawImp), "\"'`")
		if imp == "" {
			continue
		}
		if strings.HasPrefix(imp, ".") {
			resolved := filepath.ToSlash(filepath.Clean(filepath.Join(callerDir, imp)))
			if resolved == defDir || resolved == defNoExt || resolved == filepath.ToSlash(defPath) {
				if slashQual == "" || resolved == slashQual || strings.HasSuffix(resolved, "/"+slashQual) {
					return true
				}
			}
			continue
		}
		slashImp := strings.ReplaceAll(imp, ".", "/")
		if defDir != "." && (imp == defDir || strings.HasSuffix(imp, "/"+defDir) || slashImp == defDir || strings.HasSuffix(slashImp, "/"+defDir)) {
			if slashQual == "" || defDir == slashQual || strings.HasSuffix(defDir, "/"+slashQual) {
				return true
			}
		}
	}
	return false
}

func isCommonBuiltin(name string) bool {
	switch name {
	case "append", "cap", "clear", "close", "complex", "copy", "delete",
		"error", "imag", "len", "log", "make", "max", "min", "new",
		"panic", "print", "printf", "println", "real", "recover", "require":
		return true
	default:
		return false
	}
}
