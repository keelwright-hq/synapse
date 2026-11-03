package index

import (
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// Route operation props. Code routes are operation nodes like spec
// operations, marked source=code, so contract readers (resolve_api) see them.
const (
	PropOpSource    = "source"
	OpSourceCode    = "code"
	PropOpHandler   = "handler" // node ID of the function serving the route
	PropOpRoute     = "route"   // METHOD and path as declared in the route's file
	PropOpUnmounted = "unmounted"
	maxMountDepth   = 4
	maxWrapperDepth = 5
)

// resolveHTTP turns the parser's HTTP records into graph facts: one operation
// node per route at its full path (router prefixes and mounts composed across
// files), and on every caller the resolved client calls (http_clients), with
// calls through wrapper functions followed up to maxWrapperDepth levels. The
// workspace binder then matches clients to routes.
func (idx *Indexer) resolveHTTP(repo string, log *slog.Logger) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	files := map[string]bool{}
	modules := map[string]graph.Node{}
	byName := map[string][]graph.Node{}
	var oldRoutes []graph.NodeID
	var callers []graph.Node
	err := idx.store.ForEachNode(func(n graph.Node) bool {
		switch n.Kind {
		case parse.KindFile:
			if n.Path != "" {
				files[n.Path] = true
			}
		case parse.KindModule:
			modules[n.Path] = n
		case parse.KindFunction, parse.KindMethod:
			if n.Name != "" {
				byName[n.Name] = append(byName[n.Name], n)
			}
		case parse.KindOperation:
			if n.Props[PropOpSource] == OpSourceCode {
				oldRoutes = append(oldRoutes, n.ID)
			}
		}
		if n.Props[parse.PropHTTPCalls] != "" || n.Props[parse.PropHTTPClients] != "" {
			callers = append(callers, n)
		}
		return true
	})
	if err != nil {
		return err
	}
	for _, id := range oldRoutes {
		if err := idx.store.DeleteNode(id); err != nil && err != graph.ErrNotFound {
			return err
		}
	}

	routes := composeRoutes(modules, files)
	for _, r := range routes {
		if err := idx.putRoute(repo, r, modules, byName); err != nil {
			return err
		}
	}

	clients, err := idx.resolveClients(callers, byName)
	if err != nil {
		return err
	}
	log.Info("index resolved http", "routes", len(routes), "client_calls", clients)
	return nil
}

type routeDef struct {
	module  string
	method  string
	path    string
	local     string // the path as declared, before mount prefixes
	handler   string
	line      int
	unmounted bool
}

type mountRef struct {
	from   string // module path of the mounting file
	router string // the mounting router's variable
	prefix string
}

type routerKey struct{ module, name string }

// composeRoutes resolves each route to its full path(s): the path given at
// the route, the router's own prefix, and the prefixes of every mount that
// reaches the router, followed through imported router modules.
func composeRoutes(modules map[string]graph.Node, files map[string]bool) []routeDef {
	routers := map[routerKey]string{}
	kinds := map[routerKey]string{}
	exported := map[string]string{}
	routerCount := map[string]int{}
	paths := make([]string, 0, len(modules))
	for p := range modules {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		for _, rec := range parse.DecodeRecords(modules[p].Props[parse.PropHTTPRouters]) {
			if len(rec) < 3 {
				continue
			}
			routers[routerKey{p, rec[0]}] = rec[1]
			if len(rec) > 3 {
				kinds[routerKey{p, rec[0]}] = rec[3]
			}
			routerCount[p]++
			if rec[2] == "1" {
				exported[p] = rec[0]
			}
		}
	}
	exportedRouter := func(mod string) string {
		if r, ok := exported[mod]; ok {
			return r
		}
		if routerCount[mod] == 1 {
			for k := range routers {
				if k.module == mod {
					return k.name
				}
			}
		}
		return ""
	}

	into := map[routerKey][]mountRef{}
	for _, p := range paths {
		for _, rec := range parse.DecodeRecords(modules[p].Props[parse.PropHTTPMounts]) {
			if len(rec) < 4 {
				continue
			}
			prefix, recv, target, specs := rec[0], rec[1], rec[2], parse.SplitTokens(rec[3])
			targetMod := p
			if len(specs) > 0 {
				targetMod = ""
				for _, spec := range specs {
					if t, ok := resolveImportPath(p, spec, files); ok {
						targetMod = t
						break
					}
				}
				if targetMod == "" {
					continue
				}
			}
			if target == "" {
				target = exportedRouter(targetMod)
			}
			if target == "" {
				continue
			}
			targetMod, target = followReexports(modules, files, targetMod, target, routers)
			k := routerKey{targetMod, target}
			into[k] = append(into[k], mountRef{from: p, router: recv, prefix: prefix})
		}
	}

	var prefixes func(k routerKey, depth int, seen map[routerKey]bool) []string
	prefixes = func(k routerKey, depth int, seen map[routerKey]bool) []string {
		local := routers[k]
		ms := into[k]
		if len(ms) == 0 || depth >= maxMountDepth || seen[k] {
			return []string{local}
		}
		seen[k] = true
		defer delete(seen, k)
		var out []string
		for _, m := range ms {
			for _, pp := range prefixes(routerKey{m.from, m.router}, depth+1, seen) {
				out = append(out, pp+m.prefix+local)
			}
		}
		return out
	}

	var out []routeDef
	seen := map[string]bool{}
	add := func(r routeDef) {
		key := r.module + "\x00" + r.method + "\x00" + r.path
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, r)
	}
	for _, p := range paths {
		for _, rec := range parse.DecodeRecords(modules[p].Props[parse.PropHTTPRoutes]) {
			if len(rec) < 5 {
				continue
			}
			line, _ := strconv.Atoi(rec[4])
			k := routerKey{p, rec[2]}
			// A router nothing mounts (or whose mount was not resolved) has an
			// unknown prefix: its routes are kept but never matched.
			unmounted := len(into[k]) == 0 && (kinds[k] == "router" || kinds[k] == "" && strings.Contains(strings.ToLower(rec[2]), "router"))
			for _, pre := range prefixes(k, 0, map[routerKey]bool{}) {
				add(routeDef{module: p, method: rec[0], path: parse.NormalizeHTTPPath(pre + "/" + rec[1]), local: parse.NormalizeHTTPPath(rec[1]), handler: rec[3], line: line, unmounted: unmounted})
			}
		}
		if methods := modules[p].Props[parse.PropNextRoute]; methods != "" {
			if rp, ok := nextRoutePath(p); ok {
				for _, m := range strings.Split(methods, ",") {
					if m == "default" {
						if !strings.Contains(p, "pages/") {
							continue
						}
						m = parse.AnyMethod
					}
					add(routeDef{module: p, method: m, path: rp, local: rp, handler: m})
				}
			}
		}
	}
	return out
}

// nextRoutePath maps a Next.js route file to its URL path:
// src/app/api/users/[id]/route.ts -> /api/users/{}, pages/api/x.ts -> /api/x.
// Route groups (folder) and parallel-route slots (@folder) are dropped.
func nextRoutePath(file string) (string, bool) {
	segs := strings.Split(file, "/")
	base := segs[len(segs)-1]
	stem := strings.TrimSuffix(base, path.Ext(base))
	for i := len(segs) - 2; i >= 0; i-- {
		switch {
		case segs[i] == "app" && stem == "route":
			return nextSegments(segs[i+1 : len(segs)-1]), true
		case segs[i] == "pages" && i+1 < len(segs)-1 && segs[i+1] == "api":
			parts := append([]string{}, segs[i+1:len(segs)-1]...)
			if stem != "index" {
				parts = append(parts, stem)
			}
			return nextSegments(parts), true
		}
	}
	return "", false
}

func nextSegments(parts []string) string {
	var keep []string
	for _, s := range parts {
		if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") || strings.HasPrefix(s, "@") {
			continue
		}
		keep = append(keep, s)
	}
	return parse.NormalizeHTTPPath("/" + strings.Join(keep, "/"))
}

func (idx *Indexer) putRoute(repo string, r routeDef, modules map[string]graph.Node, byName map[string][]graph.Node) error {
	name := r.method + " " + r.path
	id := graph.NodeID("operation:" + r.module + "#" + name)
	props := map[string]string{
		"method":     r.method,
		"path":       r.path,
		PropOpSource: OpSourceCode,
		// One declared route mounted at several prefixes shares this key.
		PropOpRoute: r.method + " " + r.local,
	}
	if r.unmounted {
		props[PropOpUnmounted] = "1"
	}
	if r.line > 0 {
		props["start_line"] = strconv.Itoa(r.line)
		props["end_line"] = strconv.Itoa(r.line)
	}
	imports, err := idx.store.OutEdges(graph.NodeID("module:"+r.module), parse.EdgeImports)
	if err != nil {
		return err
	}
	imported := map[string]bool{}
	for _, e := range imports {
		if p, ok := strings.CutPrefix(string(e.To), "module:"); ok {
			imported[p] = true
		}
	}
	if h := routeHandler(r, modules, byName, imported); h != "" {
		props[PropOpHandler] = string(h)
	}
	if canonical, ok, err := uri.Assign(repo, r.module, parse.KindOperation, name, string(id)); err == nil && ok {
		props[uri.PropKey] = canonical
	}
	if err := idx.store.PutNode(graph.Node{ID: id, Kind: parse.KindOperation, Name: name, Path: r.module, Props: props}); err != nil {
		return err
	}
	return idx.store.PutEdge(graph.Edge{
		From: graph.NodeID("file:" + r.module), To: id, Type: parse.EdgeContains,
		Props: map[string]string{parse.PropProvenance: parse.ProvenanceExtracted},
	})
}

// routeHandler is the function serving a route: a function or method of
// that name in the route's file, else the only declaration of that name in
// a file the route's file imports, else the route file's module (an inline
// callback, or a handler that cannot be placed).
func routeHandler(r routeDef, modules map[string]graph.Node, byName map[string][]graph.Node, imported map[string]bool) graph.NodeID {
	if r.handler != "" {
		var local, viaImport []graph.Node
		for _, n := range byName[r.handler] {
			switch {
			case n.Path == r.module:
				local = append(local, n)
			case imported[n.Path]:
				viaImport = append(viaImport, n)
			}
		}
		if len(local) == 1 {
			return local[0].ID
		}
		if len(local) == 0 && len(viaImport) == 1 {
			return viaImport[0].ID
		}
	}
	if m, ok := modules[r.module]; ok {
		return m.ID
	}
	return ""
}

type httpWrapper struct {
	arg    int
	method string
	tokens []string
}

// resolveClients writes each caller's resolved client calls: direct calls as
// recorded, and calls into wrapper functions with the wrapper's base tokens
// and method. Wrappers of wrappers (get(path) -> request('GET', path) ->
// fetch) are found by a bounded fixed point.
func (idx *Indexer) resolveClients(callers []graph.Node, byName map[string][]graph.Node) (int, error) {
	byID := map[graph.NodeID]graph.Node{}
	for _, list := range byName {
		for _, n := range list {
			byID[n.ID] = n
		}
	}
	callEdges := map[graph.NodeID][]graph.Edge{}
	callsOf := func(id graph.NodeID) ([]graph.Edge, error) {
		if edges, ok := callEdges[id]; ok {
			return edges, nil
		}
		edges, err := idx.store.OutEdges(id, parse.EdgeCalls)
		if err != nil {
			return nil, err
		}
		callEdges[id] = edges
		return edges, nil
	}
	// helperEnv is the env vars a function reads, or the functions it calls
	// read, two calls deep (requireBase() -> getBase() -> process.env.X).
	var helperEnv func(id graph.NodeID, depth int) ([]string, error)
	helperEnv = func(id graph.NodeID, depth int) ([]string, error) {
		n, ok := byID[id]
		if !ok {
			return nil, nil
		}
		var out []string
		if env := n.Props[parse.PropEnvReads]; env != "" {
			out = strings.Split(env, ",")
		}
		if depth == 0 {
			return out, nil
		}
		edges, err := callsOf(id)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			env, err := helperEnv(e.To, depth-1)
			if err != nil {
				return nil, err
			}
			out = unionTokens(out, env)
		}
		return out, nil
	}
	// expand adds, for a token naming a function the declaration calls
	// (GETBACKENDURL from getBackendUrl()), the env vars that function reads.
	// Calls are followed by edge, from the declaration and from its file's
	// module (a client created at module level), so two same-named helpers
	// in different files never mix.
	expand := func(owner graph.NodeID, path string, tokens []string) ([]string, error) {
		out := tokens
		for _, from := range []graph.NodeID{owner, graph.NodeID("module:" + path)} {
			edges, err := callsOf(from)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				name := strings.ToUpper(memberName(e.To))
				for _, t := range tokens {
					if t != name {
						continue
					}
					env, err := helperEnv(e.To, 2)
					if err != nil {
						return nil, err
					}
					out = unionTokens(out, env)
				}
			}
		}
		return out, nil
	}

	wrappers := map[graph.NodeID]httpWrapper{}
	for _, list := range byName {
		for _, n := range list {
			for _, rec := range parse.DecodeRecords(n.Props[parse.PropHTTPWrapper]) {
				if len(rec) < 3 {
					continue
				}
				arg, err := strconv.Atoi(rec[0])
				if err != nil {
					continue
				}
				if _, ok := wrappers[n.ID]; !ok {
					tokens, err := expand(n.ID, n.Path, parse.SplitTokens(rec[2]))
					if err != nil {
						return 0, err
					}
					wrappers[n.ID] = httpWrapper{arg: arg, method: rec[1], tokens: tokens}
				}
			}
		}
	}

	targets := map[graph.NodeID][]graph.Edge{}
	calleesOf := func(caller graph.NodeID, callee string) ([]graph.NodeID, error) {
		edges, ok := targets[caller]
		if !ok {
			var err error
			edges, err = idx.store.OutEdges(caller, parse.EdgeCalls)
			if err != nil {
				return nil, err
			}
			targets[caller] = edges
		}
		last := callee
		if i := strings.LastIndex(callee, "."); i >= 0 {
			last = callee[i+1:]
		}
		var out []graph.NodeID
		for _, e := range edges {
			if strings.HasPrefix(string(e.To), "func:") || strings.HasPrefix(string(e.To), "method:") {
				if memberName(e.To) == last {
					out = append(out, e.To)
				}
			}
		}
		return out, nil
	}

	type callRec struct {
		kind, callee, method, tail string
		arg, param, line           int
		tokens                     []string
	}
	recsOf := map[graph.NodeID][]callRec{}
	for _, c := range callers {
		for _, f := range parse.DecodeRecords(c.Props[parse.PropHTTPCalls]) {
			if len(f) < 8 {
				continue
			}
			tokens, err := expand(c.ID, c.Path, parse.SplitTokens(f[4]))
			if err != nil {
				return 0, err
			}
			r := callRec{kind: f[0], callee: f[1], method: f[3], tokens: tokens, tail: f[5], param: -1}
			r.arg, _ = strconv.Atoi(f[2])
			if f[6] != "" {
				r.param, _ = strconv.Atoi(f[6])
			}
			r.line, _ = strconv.Atoi(f[7])
			recsOf[c.ID] = append(recsOf[c.ID], r)
		}
	}

	for round := 0; round < maxWrapperDepth; round++ {
		changed := false
		for _, c := range callers {
			if _, done := wrappers[c.ID]; done {
				continue
			}
			for _, r := range recsOf[c.ID] {
				if r.kind != "wrapper" || r.param < 0 {
					continue
				}
				ws, err := calleesOf(c.ID, r.callee)
				if err != nil {
					return 0, err
				}
				for _, w := range ws {
					wr, ok := wrappers[w]
					if !ok || wr.arg != r.arg {
						continue
					}
					method := r.method
					if method == parse.AnyMethod {
						method = wr.method
					}
					wrappers[c.ID] = httpWrapper{arg: r.param, method: method, tokens: unionTokens(wr.tokens, r.tokens)}
					changed = true
					break
				}
				if _, ok := wrappers[c.ID]; ok {
					break
				}
			}
		}
		if !changed {
			break
		}
	}

	total := 0
	for _, c := range callers {
		var out []string
		for _, r := range recsOf[c.ID] {
			if r.tail == "" {
				continue
			}
			switch r.kind {
			case "direct":
				out = append(out, parse.EncodeRecord(r.method, parse.JoinTokens(r.tokens), r.tail, strconv.Itoa(r.line)))
			case "wrapper":
				ws, err := calleesOf(c.ID, r.callee)
				if err != nil {
					return 0, err
				}
				matched := false
				for _, w := range ws {
					wr, ok := wrappers[w]
					if !ok || wr.arg != r.arg {
						continue
					}
					method := r.method
					if method == parse.AnyMethod {
						method = wr.method
					}
					out = append(out, parse.EncodeRecord(method, parse.JoinTokens(unionTokens(wr.tokens, r.tokens)), r.tail, strconv.Itoa(r.line)))
					matched = true
					break
				}
				// An unresolved receiver (clients.insights.get('/x')) whose
				// method is an HTTP verb is still a likely request.
				if !matched && isVerbCallee(r.callee) {
					out = append(out, parse.EncodeRecord(r.method, parse.JoinTokens(r.tokens), r.tail, strconv.Itoa(r.line)))
				}
			}
		}
		sort.Strings(out)
		out = dedupStrings(out)
		joined := strings.Join(out, ",")
		if joined == c.Props[parse.PropHTTPClients] {
			total += len(out)
			continue
		}
		n, err := idx.store.GetNode(c.ID)
		if err != nil {
			continue
		}
		if n.Props == nil {
			n.Props = map[string]string{}
		}
		if joined == "" {
			delete(n.Props, parse.PropHTTPClients)
		} else {
			n.Props[parse.PropHTTPClients] = joined
		}
		if err := idx.store.PutNode(n); err != nil {
			return 0, err
		}
		total += len(out)
	}
	return total, nil
}

// memberName is the declared name in a func:/method: node ID (Type.m -> m).
func memberName(id graph.NodeID) string {
	s := string(id)
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func isVerbCallee(callee string) bool {
	i := strings.LastIndex(callee, ".")
	if i < 0 {
		return false
	}
	switch callee[i+1:] {
	case "get", "post", "put", "patch", "delete":
		return true
	}
	return false
}

func unionTokens(a, b []string) []string {
	out := append(append([]string{}, a...), b...)
	sort.Strings(out)
	return dedupStrings(out)
}

func dedupStrings(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// followReexports follows a mounted name that a package only re-exports
// (`from .users import router as users_router` in __init__.py) to the module
// that creates the router.
func followReexports(modules map[string]graph.Node, files map[string]bool, mod, name string, routers map[routerKey]string) (string, string) {
	for hop := 0; hop < 3; hop++ {
		if _, ok := routers[routerKey{mod, name}]; ok {
			return mod, name
		}
		next := ""
		nextName := ""
		for _, rec := range parse.DecodeRecords(modules[mod].Props[parse.PropHTTPImports]) {
			if len(rec) < 3 || rec[0] != name {
				continue
			}
			for _, spec := range parse.SplitTokens(rec[2]) {
				if t, ok := resolveImportPath(mod, spec, files); ok && t != mod {
					next, nextName = t, rec[1]
					break
				}
			}
		}
		if next == "" {
			return mod, name
		}
		mod, name = next, nextName
	}
	return mod, name
}
