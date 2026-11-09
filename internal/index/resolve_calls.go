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
	// implsByName leaves out protocol requirements, so the name-based tiers
	// see the same candidates they always did.
	implsByName := make(map[string][]graph.Node)
	swiftPropTypes := map[string]map[string][]string{}
	typesByFamily := map[string]map[string][]graph.Node{}
	importsByFile := make(map[string][]string)
	var callers, typeUsers []graph.Node
	existingSymbols := make(map[graph.NodeID]bool)

	err := idx.store.ForEachNode(func(n graph.Node) bool {
		switch n.Kind {
		case parse.KindFunction, parse.KindMethod:
			if n.Name != "" {
				defsByName[n.Name] = append(defsByName[n.Name], n)
				if n.Props[parse.PropRequirement] == "" {
					implsByName[n.Name] = append(implsByName[n.Name], n)
				}
			}
		case parse.KindType:
			// Stored properties of a Swift type and its extensions, by type name.
			if isSwiftPath(n.Path) && n.Name != "" && n.Props[parse.PropVarTypes] != "" {
				if swiftPropTypes[n.Name] == nil {
					swiftPropTypes[n.Name] = map[string][]string{}
				}
				addVarTypes(swiftPropTypes[n.Name], n.Props[parse.PropVarTypes])
			}
			if fam := languageFamily(n.Path); n.Name != "" && fam != "" && n.Props[parse.PropExtension] == "" {
				if typesByFamily[fam] == nil {
					typesByFamily[fam] = map[string][]graph.Node{}
				}
				typesByFamily[fam][n.Name] = append(typesByFamily[fam][n.Name], n)
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
		if n.Props != nil && (n.Props[parse.PropTypeRefs] != "" || n.Props[parse.PropSupertypes] != "") {
			typeUsers = append(typeUsers, n)
		}
		return true
	})
	if err != nil {
		return err
	}

	swiftTypesByName := typesByFamily["swift"]
	byNames := []map[string][]graph.Node{defsByName, implsByName}
	for _, m := range typesByFamily {
		byNames = append(byNames, m)
	}
	for _, byName := range byNames {
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
			// A Swift call through a value with a declared type goes to that
			// type's method. A concrete type settles it; a protocol requirement
			// is recorded next to whatever the name-based tiers find.
			typedOK := false
			if isSwiftPath(caller.Path) {
				if id, ok := pickSwiftTypedReceiver(caller, calleeName, defsByName[memberName], swiftPropTypes); ok {
					typedOK = true
					if id != caller.ID {
						desired[id] = parse.ProvenanceInferred
					}
					if !isRequirementIn(defsByName[memberName], id) {
						continue
					}
				}
			}
			targetID, prov, ok := pickCalleeTarget(caller, calleeName, implsByName[memberName], fileImports)
			if !ok && isSwiftPath(caller.Path) {
				targetID, prov, ok = pickSwiftModuleTarget(caller, calleeName, implsByName[memberName], swiftTypesByName)
			}
			if ok {
				if targetID != caller.ID {
					desired[targetID] = prov
				}
				continue
			}
			if typedOK {
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

	sort.Slice(typeUsers, func(i, j int) bool { return typeUsers[i].ID < typeUsers[j].ID })
	for _, user := range typeUsers {
		if err := idx.resolveTypeEdges(user, typesByFamily[languageFamily(user.Path)]); err != nil {
			return err
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

// resolveTypeEdges links a declaration's recorded type names to type nodes:
// type_refs become references edges and supertypes become inherits edges.
// Names resolve within the declaration's language family (typesByName), and
// only when exactly one repo type declares them (String, Codable, Promise
// produce no edge and no placeholder node).
func (idx *Indexer) resolveTypeEdges(user graph.Node, typesByName map[string][]graph.Node) error {
	for _, spec := range []struct {
		prop string
		typ  graph.EdgeType
	}{
		{parse.PropTypeRefs, parse.EdgeReferences},
		{parse.PropSupertypes, parse.EdgeInherits},
	} {
		desired := map[graph.NodeID]string{}
		for _, name := range strings.Split(user.Props[spec.prop], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			types := typesByName[name]
			if len(types) != 1 || types[0].ID == user.ID {
				continue
			}
			// A module using a type it declares itself says nothing about impact.
			if user.Kind == parse.KindModule && types[0].Path == user.Path {
				continue
			}
			prov := parse.ProvenanceInferred
			if types[0].Path == user.Path {
				prov = parse.ProvenanceExtracted
			}
			desired[types[0].ID] = prov
		}
		existing, err := idx.store.OutEdges(user.ID, spec.typ)
		if err != nil {
			return err
		}
		for _, e := range existing {
			if _, keep := desired[e.To]; !keep {
				if err := idx.store.DeleteEdge(e.From, e.To, e.Type); err != nil && err != graph.ErrNotFound {
					return err
				}
			}
		}
		targets := make([]graph.NodeID, 0, len(desired))
		for id := range desired {
			targets = append(targets, id)
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
		for _, to := range targets {
			if err := idx.store.PutEdge(graph.Edge{
				From:  user.ID,
				To:    to,
				Type:  spec.typ,
				Props: map[string]string{parse.PropProvenance: desired[to]},
			}); err != nil {
				return err
			}
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

// addVarTypes adds the "name:Type" pairs of a var_types prop to byName.
func addVarTypes(byName map[string][]string, raw string) {
	for _, pair := range strings.Split(raw, ",") {
		name, typ, ok := strings.Cut(pair, ":")
		if !ok || name == "" || typ == "" {
			continue
		}
		known := false
		for _, t := range byName[name] {
			if t == typ {
				known = true
			}
		}
		if !known {
			byName[name] = append(byName[name], typ)
		}
	}
}

// pickSwiftTypedReceiver resolves `x.method()` and `self.x.method()` through
// the declared type of x: a parameter or local of the caller, or a stored
// property of the caller's type. It links only when x has exactly one known
// type and that type declares exactly one method of the name. A protocol type
// gives its requirement.
func pickSwiftTypedReceiver(caller graph.Node, calleeName string, candidates []graph.Node, propTypes map[string]map[string][]string) (graph.NodeID, bool) {
	qualifier, _ := splitCalleeQualifier(calleeName)
	if qualifier == "" {
		return "", false
	}
	segs := strings.Split(qualifier, ".")
	viaSelf := segs[0] == "self"
	if viaSelf {
		segs = segs[1:]
	}
	if len(segs) != 1 {
		return "", false
	}
	name := strings.TrimRight(segs[0], "?!")
	if name == "" {
		return "", false
	}
	var types []string
	if !viaSelf {
		local := map[string][]string{}
		addVarTypes(local, caller.Props[parse.PropVarTypes])
		types = local[name]
	}
	if len(types) == 0 {
		if recv := methodReceiver(caller); recv != "" {
			types = propTypes[recv][name]
		}
	}
	if len(types) != 1 {
		return "", false
	}
	var match []graph.Node
	for _, c := range candidates {
		if c.Kind == parse.KindMethod && isSwiftPath(c.Path) && methodReceiver(c) == types[0] {
			match = append(match, c)
		}
	}
	if len(match) != 1 {
		return "", false
	}
	return match[0].ID, true
}

// isRequirementIn reports whether id is a protocol requirement among nodes.
func isRequirementIn(nodes []graph.Node, id graph.NodeID) bool {
	for _, n := range nodes {
		if n.ID == id {
			return n.Props[parse.PropRequirement] != ""
		}
	}
	return false
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

// languageFamily groups files whose type names resolve against each other:
// Swift; JavaScript and TypeScript; Python. Other languages have no type edges.
func languageFamily(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".swift":
		return "swift"
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return "js"
	case ".py":
		return "py"
	}
	return ""
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
