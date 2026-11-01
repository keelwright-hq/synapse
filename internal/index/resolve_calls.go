package index

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

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
	swiftTypesByName := make(map[string][]graph.Node)
	importsByFile := make(map[string][]string)
	var callers []graph.Node
	existingSymbols := make(map[graph.NodeID]bool)

	err := idx.store.ForEachNode(func(n graph.Node) bool {
		switch n.Kind {
		case parse.KindFunction, parse.KindMethod:
			if n.Name != "" {
				defsByName[n.Name] = append(defsByName[n.Name], n)
			}
		case parse.KindType:
			if n.Name != "" && isSwiftPath(n.Path) && n.Props[parse.PropExtension] == "" {
				swiftTypesByName[n.Name] = append(swiftTypesByName[n.Name], n)
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

	for _, byName := range []map[string][]graph.Node{defsByName, swiftTypesByName} {
		for name, defs := range byName {
			sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
			byName[name] = defs
		}
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
			targetID, prov, ok := pickCalleeTarget(caller, calleeName, defsByName[memberName], fileImports)
			if !ok && isSwiftPath(caller.Path) {
				targetID, prov, ok = pickSwiftModuleTarget(caller, calleeName, defsByName[memberName], swiftTypesByName)
			}
			if ok {
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

		// Tier 1b: Same receiver type across files (e.g. Swift extensions, C# partial types).
		if recv := implicitSelfReceiver(caller); recv != "" {
			var sameRecv []graph.Node
			for _, c := range candidates {
				if methodReceiver(c) == recv {
					sameRecv = append(sameRecv, c)
				}
			}
			if len(sameRecv) == 1 {
				return sameRecv[0].ID, parse.ProvenanceInferred, true
			}
			if len(sameRecv) > 1 {
				return "", "", false
			}
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

	// Tier 3b: Type-qualified call (Type.method, Type.shared.method, self.method)
	// where exactly one candidate's receiver is named by the qualifier.
	if !isBare {
		if id, ok := pickByQualifierReceiver(caller, qualifier, candidates); ok {
			return id, parse.ProvenanceInferred, true
		}
	}

	// Tier 4: Unique top-level function across the repository (excluding common language builtins).
	if isBare && !isCommonBuiltin(calleeName) && len(candidates) == 1 && candidates[0].Kind == parse.KindFunction {
		return candidates[0].ID, parse.ProvenanceInferred, true
	}

	return "", "", false
}

// pickByQualifierReceiver matches a qualified call to the one candidate method
// whose receiver type is a segment of the qualifier. Bare "self" and "this"
// qualifiers stand for the caller's own receiver; chained field accesses like
// "self.store.refreshAll" do not map "self" to the caller's receiver.
func pickByQualifierReceiver(caller graph.Node, qualifier string, candidates []graph.Node) (graph.NodeID, bool) {
	segs := map[string]bool{}
	switch qualifier {
	case "self", "this", "Self":
		if recv := methodReceiver(caller); recv != "" {
			segs[recv] = true
		}
	default:
		for i, seg := range strings.FieldsFunc(qualifier, func(r rune) bool { return r == '.' || r == ':' }) {
			switch seg {
			case "self", "this":
				// Property chain on self/this: the method receiver is the property, not caller.
			case "Self":
				if i == 0 {
					if recv := methodReceiver(caller); recv != "" {
						segs[recv] = true
					}
				}
			default:
				segs[seg] = true
			}
		}
	}
	var match []graph.Node
	for _, c := range candidates {
		if c.Kind == parse.KindMethod && segs[methodReceiver(c)] {
			match = append(match, c)
		}
	}
	if len(match) == 1 {
		return match[0].ID, true
	}
	return "", false
}

// pickSwiftModuleTarget resolves calls the generic tiers cannot: every file in a
// Swift module sees every other without imports, so a name that is unique among
// the repository's Swift declarations is taken as the target. Bare calls to a
// type name are initializers and link to the type. Qualifiers rooted at an
// unknown capitalized name are treated as external types and left unresolved.
func pickSwiftModuleTarget(caller graph.Node, calleeName string, candidates []graph.Node, swiftTypesByName map[string][]graph.Node) (graph.NodeID, string, bool) {
	qualifier, member := splitCalleeQualifier(calleeName)
	var swiftDefs []graph.Node
	for _, c := range candidates {
		if isSwiftPath(c.Path) {
			swiftDefs = append(swiftDefs, c)
		}
	}

	if qualifier == "" {
		if recv := methodReceiver(caller); recv != "" {
			var sameRecv []graph.Node
			for _, c := range swiftDefs {
				if methodReceiver(c) == recv {
					sameRecv = append(sameRecv, c)
				}
			}
			if len(sameRecv) == 1 {
				return sameRecv[0].ID, parse.ProvenanceInferred, true
			}
		}
		if len(swiftDefs) == 1 {
			return swiftDefs[0].ID, parse.ProvenanceInferred, true
		}
		if len(swiftDefs) == 0 {
			if types := swiftTypesByName[member]; len(types) == 1 {
				return types[0].ID, parse.ProvenanceInferred, true
			}
		}
		return "", "", false
	}

	switch qualifier {
	case "self", "this", "Self", "super":
		// Direct receiver calls were already checked against caller's receiver by Tier 3b.
		return "", "", false
	}

	root := qualifier
	if i := strings.IndexByte(root, '.'); i >= 0 {
		root = root[:i]
	}
	if root == "" || root == "_" {
		return "", "", false
	}
	if unicode.IsUpper([]rune(root)[0]) {
		if len(swiftTypesByName[root]) == 0 {
			return "", "", false
		}
		// A direct Type.method() call was already checked against Type by Tier 3b;
		// do not fall through to an unrelated type's method of the same name.
		if !strings.Contains(qualifier, ".") {
			if member == "init" && len(swiftDefs) == 0 && len(swiftTypesByName[qualifier]) == 1 {
				return swiftTypesByName[qualifier][0].ID, parse.ProvenanceInferred, true
			}
			return "", "", false
		}
	}
	if len(swiftDefs) == 1 {
		return swiftDefs[0].ID, parse.ProvenanceInferred, true
	}
	if len(swiftDefs) > 1 {
		lastSeg := qualifier
		if i := strings.LastIndexByte(lastSeg, '.'); i >= 0 {
			lastSeg = lastSeg[i+1:]
		}
		// Match the property name to the end of the receiver type, so
		// `repository.save` picks WeatherRepository.save; ambiguity stays unresolved.
		lastSeg = strings.ToLower(lastSeg)
		var byPropName []graph.Node
		for _, c := range swiftDefs {
			if lastSeg != "" && strings.HasSuffix(strings.ToLower(methodReceiver(c)), lastSeg) {
				byPropName = append(byPropName, c)
			}
		}
		if len(byPropName) == 1 {
			return byPropName[0].ID, parse.ProvenanceInferred, true
		}
	}
	return "", "", false
}

// methodReceiver returns the receiver type encoded in a method node ID
// ("method:path#Recv.name" or "method:path#Recv.name(sig)"), or "".
func methodReceiver(n graph.Node) string {
	if n.Kind != parse.KindMethod {
		return ""
	}
	if n.Props != nil && n.Props["receiver"] != "" {
		return n.Props["receiver"]
	}
	id := string(n.ID)
	i := strings.LastIndexByte(id, '#')
	if i < 0 {
		return ""
	}
	sym := id[i+1:]
	if j := strings.IndexByte(sym, '('); j >= 0 {
		sym = sym[:j]
	}
	if j := strings.LastIndexByte(sym, '.'); j > 0 {
		return sym[:j]
	}
	return ""
}

// implicitSelfReceiver returns the caller's receiver type when its language
// resolves a bare call inside a method against the method's own type first
// (Swift, Kotlin, Java, C#, C++, Ruby). In Go, Python, JavaScript, TypeScript
// and PHP a bare call never means self.method, so it returns "".
func implicitSelfReceiver(caller graph.Node) string {
	switch strings.ToLower(filepath.Ext(caller.Path)) {
	case ".swift", ".kt", ".kts", ".java", ".cs", ".rb",
		".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx":
		return methodReceiver(caller)
	default:
		return ""
	}
}

func isSwiftPath(p string) bool {
	return strings.EqualFold(filepath.Ext(p), ".swift")
}

func disambiguateCandidates(caller graph.Node, candidates []graph.Node) (graph.NodeID, bool) {
	if recv := implicitSelfReceiver(caller); recv != "" {
		var byRecv []graph.Node
		for _, c := range candidates {
			if methodReceiver(c) == recv {
				byRecv = append(byRecv, c)
			}
		}
		if len(byRecv) == 1 {
			return byRecv[0].ID, true
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
