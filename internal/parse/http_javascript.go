package parse

import (
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// extractHTTPJS records the routes, router mounts and HTTP client calls in a
// JS or TS file (Express, Hono, Next.js route files; fetch, axios, got, ky
// and functions wrapping them). Strings are evaluated statically and only as
// far as literals, templates, `+`, same-file constants and the enclosing
// function's parameters go; anything else becomes a token-carrying dynamic
// value (the base URL) or a {} path parameter.
func extractHTTPJS(b *builder, root *tree_sitter.Node, module graph.NodeID) {
	x := &jsHTTP{
		rec:       newHTTPRecorder(b, module),
		b:         b,
		consts:    map[string]*tree_sitter.Node{},
		bindings:  map[string]string{},
		named:     map[string]string{},
		routers:   map[string]string{},
		instances: map[string][]string{},
		helpers:   map[string]*mountHelper{},
		funcEnv:   map[string][]string{},
	}
	x.prepass(root)
	x.walk(root, nil)
	x.recordNextRoute(root)
	for name, kind := range x.routers {
		x.rec.router(name, "", x.exported[name], kind)
	}
}

type jsHTTP struct {
	rec *httpRecorder
	b   *builder
	// consts are same-file `const X = <expr>` and class field values by name.
	consts map[string]*tree_sitter.Node
	// bindings map a local name to the module it was required or imported from.
	bindings map[string]string
	// named maps a name imported by name (`const { x } = require(...)`,
	// `import { y as x }`) to its exported name (x -> y).
	named map[string]string
	// routers are Express/Hono/Koa apps ("app") and routers ("router")
	// created in this file.
	routers  map[string]string
	exported map[string]bool
	// instances are HTTP clients with a base URL (axios.create), by name.
	instances map[string][]string
	// helpers are same-file functions that mount their arguments
	// (`function mount(suffix, ...h) { app.use(`/api${suffix}`, ...h) }`).
	helpers map[string]*mountHelper
	// funcEnv are the env vars a same-file function reads (URL normalizers).
	funcEnv map[string][]string
}

type mountHelper struct {
	params map[string]int
	mounts []helperMount
}

type helperMount struct {
	router string
	prefix *tree_sitter.Node
}

type jsScope struct {
	fn     *tree_sitter.Node
	params map[string]int
	consts map[string]*tree_sitter.Node
	parent *jsScope
}

func (s *jsScope) node() *tree_sitter.Node {
	if s == nil {
		return nil
	}
	return s.fn
}

var jsRouterFactories = map[string]bool{
	"express": true, "Router": true, "express.Router": true, "Hono": true, "Koa": true,
	"KoaRouter": true, "fastify": true, "Fastify": true, "OpenAPIHono": true,
}

var jsHTTPLibs = map[string]bool{
	"axios": true, "got": true, "ky": true, "superagent": true, "needle": true, "request": true,
}

func (x *jsHTTP) prepass(root *tree_sitter.Node) {
	x.exported = map[string]bool{}
	// Functions first, so a client created with `baseURL: getBase()` carries
	// the env vars getBase reads.
	walkNamed(root, func(n *tree_sitter.Node) {
		switch n.Kind() {
		case "function_declaration", "generator_function_declaration":
			if name := field(n, "name"); name != nil {
				x.function(x.b.text(name), n)
			}
		case "variable_declarator":
			if name, value := field(n, "name"), field(n, "value"); name != nil && value != nil && name.Kind() == "identifier" {
				switch value.Kind() {
				case "arrow_function", "function_expression", "function":
					x.function(x.b.text(name), value)
				}
			}
		}
	})
	walkNamed(root, func(n *tree_sitter.Node) {
		switch n.Kind() {
		case "variable_declarator":
			name, value := field(n, "name"), field(n, "value")
			if name == nil || value == nil {
				return
			}
			x.declare(name, value, n.Parent() != nil && n.Parent().Parent() != nil && isJSTopLevel(n))
		case "assignment_expression":
			left, right := field(n, "left"), field(n, "right")
			if left == nil || right == nil {
				return
			}
			lt := x.b.text(left)
			if lt == "module.exports" || lt == "exports.default" {
				if right.Kind() == "identifier" {
					x.exported[x.b.text(right)] = true
				}
				return
			}
			if f := factoryName(x.b, right); f != "" {
				x.newInstance(lt, right, f)
			}
		case "public_field_definition", "field_definition":
			if name, value := field(n, "name"), field(n, "value"); name != nil && value != nil {
				if f := factoryName(x.b, value); f != "" {
					x.newInstance("this."+x.b.text(name), value, f)
				} else if isStringish(value) {
					x.consts[x.b.text(name)] = value
				}
			}
		case "import_statement":
			x.importBindings(n)
		case "export_statement":
			if d := field(n, "declaration"); d == nil {
				// export default router;
				if v := field(n, "value"); v != nil && v.Kind() == "identifier" {
					x.exported[x.b.text(v)] = true
				}
			}
		}
	})
}

func isJSTopLevel(decl *tree_sitter.Node) bool {
	p := decl.Parent() // lexical_declaration
	if p == nil {
		return false
	}
	g := p.Parent()
	return g != nil && (g.Kind() == "program" || g.Kind() == "export_statement")
}

func (x *jsHTTP) declare(name, value *tree_sitter.Node, topLevel bool) {
	if name.Kind() == "object_pattern" {
		// const { a, b } = require('./x')
		if spec := requireSpec(x.b, value); spec != "" {
			walkNamed(name, func(c *tree_sitter.Node) {
				if c.Kind() == "shorthand_property_identifier_pattern" || c.Kind() == "identifier" {
					x.bindings[x.b.text(c)] = spec
					x.named[x.b.text(c)] = x.b.text(c)
				}
			})
		}
		return
	}
	if name.Kind() != "identifier" {
		return
	}
	n := x.b.text(name)
	if spec := requireSpec(x.b, value); spec != "" {
		x.bindings[n] = spec
		return
	}
	if f := factoryName(x.b, value); f != "" {
		x.newInstance(n, value, f)
		return
	}
	switch value.Kind() {
	case "arrow_function", "function_expression", "function":
		return
	}
	if topLevel && isStringish(value) {
		x.consts[n] = value
	}
}

// requireSpec returns the module of `require('x')` or `require('x').y`.
func requireSpec(b *builder, v *tree_sitter.Node) string {
	for v != nil && v.Kind() == "member_expression" {
		v = field(v, "object")
	}
	if v == nil || v.Kind() != "call_expression" || !isRequireCall(b, field(v, "function")) {
		return ""
	}
	return firstStringArg(b, v)
}

// factoryName names what creates value: an app (express(), new Hono()), a
// router (express.Router()) or an HTTP client (axios.create), else "".
func factoryName(b *builder, v *tree_sitter.Node) string {
	switch v.Kind() {
	case "new_expression":
		c := field(v, "constructor")
		if c != nil && jsRouterFactories[b.text(c)] {
			return jsRouterKind(b.text(c))
		}
	case "call_expression":
		fn := field(v, "function")
		t := b.text(fn)
		if jsRouterFactories[t] || strings.HasSuffix(t, ".Router") {
			return jsRouterKind(t)
		}
		switch t {
		case "axios.create", "got.extend", "ky.create", "ky.extend":
			return "client"
		}
	}
	return ""
}

// jsRouterKind tells an app, whose routes are at the root unless something
// mounts it, from a router, whose routes are only reachable where mounted.
func jsRouterKind(factory string) string {
	if strings.HasSuffix(factory, "Router") {
		return "router"
	}
	return "app"
}

func (x *jsHTTP) newInstance(name string, v *tree_sitter.Node, kind string) {
	if kind == "router" || kind == "app" {
		x.routers[name] = kind
		return
	}
	var tokens []string
	if args := field(v, "arguments"); args != nil {
		walkNamed(args, func(c *tree_sitter.Node) {
			if c.Kind() != "pair" {
				return
			}
			switch strings.Trim(x.b.text(field(c, "key")), `"'`) {
			case "baseURL", "baseUrl", "prefixUrl", "prefixURL":
				tokens = append(tokens, x.eval(field(c, "value"), nil, 0).tokens()...)
			}
		})
	}
	x.instances[name] = cleanTokens(tokens)
}

func isStringish(v *tree_sitter.Node) bool {
	switch v.Kind() {
	case "string", "template_string", "binary_expression", "member_expression", "parenthesized_expression", "call_expression", "subscript_expression":
		return true
	}
	return false
}

func (x *jsHTTP) importBindings(n *tree_sitter.Node) {
	src := field(n, "source")
	if src == nil {
		return
	}
	spec := unquote(x.b.text(src))
	walkNamed(n, func(c *tree_sitter.Node) {
		switch c.Kind() {
		case "import_clause":
			for i := uint(0); i < c.NamedChildCount(); i++ {
				if id := c.NamedChild(i); id != nil && id.Kind() == "identifier" {
					x.bindings[x.b.text(id)] = spec // default import
				}
			}
		case "namespace_import":
			for i := uint(0); i < c.NamedChildCount(); i++ {
				if id := c.NamedChild(i); id != nil && id.Kind() == "identifier" {
					x.bindings[x.b.text(id)] = spec
				}
			}
		case "import_specifier":
			orig := field(c, "name")
			name := field(c, "alias")
			if name == nil {
				name = orig
			}
			if name != nil && orig != nil {
				x.bindings[x.b.text(name)] = spec
				x.named[x.b.text(name)] = x.b.text(orig)
			}
		}
	})
}

// function records what a same-file function means for HTTP: the env vars it
// reads (so `${getBaseUrl()}/x` carries them) and, for a mount helper, the
// prefixes it mounts its arguments at.
func (x *jsHTTP) function(name string, fn *tree_sitter.Node) {
	params := jsParams(x.b, fn)
	var env []string
	h := &mountHelper{params: params}
	walkNamed(field(fn, "body"), func(c *tree_sitter.Node) {
		if v := envName(x.b, c); v != "" {
			env = append(env, v)
		}
		if c.Kind() != "call_expression" {
			return
		}
		f := field(c, "function")
		if f == nil || f.Kind() != "member_expression" || x.b.text(field(f, "property")) != "use" {
			return
		}
		args := callArgs(c)
		if len(args) == 0 {
			return
		}
		usesParam := false
		walkNamed(args[0], func(id *tree_sitter.Node) {
			if id.Kind() == "identifier" {
				if _, ok := params[x.b.text(id)]; ok {
					usesParam = true
				}
			}
		})
		if usesParam {
			h.mounts = append(h.mounts, helperMount{router: x.b.text(field(f, "object")), prefix: args[0]})
		}
	})
	x.funcEnv[name] = env
	if len(h.mounts) > 0 {
		x.helpers[name] = h
	}
}

func jsParams(b *builder, fn *tree_sitter.Node) map[string]int {
	out := map[string]int{}
	ps := field(fn, "parameters")
	if ps == nil {
		if p := field(fn, "parameter"); p != nil { // x => ...
			out[b.text(p)] = 0
		}
		return out
	}
	idx := 0
	for i := uint(0); i < ps.NamedChildCount(); i++ {
		p := ps.NamedChild(i)
		if p == nil || p.Kind() == "comment" {
			continue
		}
		name := p
		switch p.Kind() {
		case "required_parameter", "optional_parameter":
			name = field(p, "pattern")
		case "assignment_pattern":
			name = field(p, "left")
		case "rest_pattern":
			name = p.NamedChild(0)
		}
		if name != nil && name.Kind() == "identifier" {
			out[b.text(name)] = idx
		}
		idx++
	}
	return out
}

// envName returns X for process.env.X, process.env['X'] and
// import.meta.env.X, and for an upper-case config field (config.BACKEND_URL)
// loaded from the variable of that name by a validated config object.
func envName(b *builder, n *tree_sitter.Node) string {
	switch n.Kind() {
	case "member_expression":
		obj := b.text(field(n, "object"))
		prop := b.text(field(n, "property"))
		if obj == "process.env" || obj == "import.meta.env" {
			return prop
		}
		if isEnvStyleName(prop) && (strings.HasSuffix(strings.ToLower(obj), "config") || strings.HasSuffix(strings.ToLower(obj), "env") || strings.HasSuffix(strings.ToLower(obj), "settings")) {
			return prop
		}
	case "subscript_expression":
		obj := b.text(field(n, "object"))
		if obj == "process.env" || obj == "import.meta.env" {
			if idx := field(n, "index"); idx != nil && idx.Kind() == "string" {
				return unquote(b.text(idx))
			}
		}
	}
	return ""
}

func callArgs(call *tree_sitter.Node) []*tree_sitter.Node {
	args := field(call, "arguments")
	if args == nil {
		return nil
	}
	var out []*tree_sitter.Node
	for i := uint(0); i < args.NamedChildCount(); i++ {
		if a := args.NamedChild(i); a != nil && a.Kind() != "comment" {
			out = append(out, a)
		}
	}
	return out
}

func (x *jsHTTP) walk(n *tree_sitter.Node, scope *jsScope) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "function_declaration", "generator_function_declaration", "function_expression", "function",
		"arrow_function", "method_definition":
		scope = &jsScope{fn: n, params: jsParams(x.b, n), consts: map[string]*tree_sitter.Node{}, parent: scope}
	case "variable_declarator":
		if scope != nil {
			if name, value := field(n, "name"), field(n, "value"); name != nil && name.Kind() == "identifier" {
				scope.consts[x.b.text(name)] = value // nil until assigned
			}
		}
	case "assignment_expression":
		// `let base; ... base = getBase();` keeps the latest value in scope.
		if left, right := field(n, "left"), field(n, "right"); scope != nil && left != nil && right != nil && left.Kind() == "identifier" {
			for s := scope; s != nil; s = s.parent {
				if _, ok := s.consts[x.b.text(left)]; ok {
					s.consts[x.b.text(left)] = right
					break
				}
			}
		}
	case "member_expression", "subscript_expression":
		if v := envName(x.b, n); v != "" {
			x.rec.envRead(v, scope.node(), lineOf(n))
		}
	case "call_expression":
		x.call(n, scope)
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		x.walk(n.NamedChild(i), scope)
	}
}

func (x *jsHTTP) call(n *tree_sitter.Node, scope *jsScope) {
	fn := field(n, "function")
	if fn != nil && fn.Kind() == "await_expression" && fn.NamedChildCount() > 0 {
		fn = fn.NamedChild(0) // `await x.get<T>(path)` parses with the await around the callee
	}
	if fn == nil {
		return
	}
	args := callArgs(n)
	line := lineOf(n)
	switch fn.Kind() {
	case "identifier":
		name := x.b.text(fn)
		switch {
		case name == "fetch" || x.funcEnv[name] == nil && strings.Contains(strings.ToLower(name), "fetch") && !strings.HasPrefix(name, "use"):
			// fetch, or an injected fetch (fetchImpl, fetchFn) not defined here.
			if len(args) > 0 {
				x.rec.direct(jsMethodOption(x.b, args, 1, "GET"), x.eval(args[0], scope, 0), nil, line, scope.node())
			}
		case name == "axios":
			x.axiosConfigCall(args, scope, line)
		case x.helpers[name] != nil:
			x.expandHelper(x.helpers[name], args, scope)
		case name != "require":
			x.wrapperCalls(name, args, scope, line)
		}
	case "member_expression":
		obj := field(fn, "object")
		prop := x.b.text(field(fn, "property"))
		recv := x.b.text(obj)
		switch {
		case recv == "window" || recv == "globalThis":
			if prop == "fetch" && len(args) > 0 {
				x.rec.direct(jsMethodOption(x.b, args, 1, "GET"), x.eval(args[0], scope, 0), nil, line, scope.node())
			}
		case x.routers[recv] != "" || jsLooksLikeRoute(x.b, prop, args):
			x.routeOrMount(recv, prop, args, line, scope)
		case obj != nil && obj.Kind() == "call_expression" && x.b.text(field(field(obj, "function"), "property")) == "route" && isHTTPVerb(prop):
			// router.route('/x').get(h)
			inner := field(obj, "function")
			if ia := callArgs(obj); len(ia) > 0 {
				if p, ok := x.eval(ia[0], scope, 0).literal(); ok {
					x.rec.route(strings.ToUpper(prop), p, x.b.text(field(inner, "object")), jsHandlerName(x.b, args), line)
				}
			}
		case jsHTTPLibs[recv] || x.instances[recv] != nil:
			x.libCall(recv, prop, args, scope, line)
		case isHTTPVerb(prop) || prop == "request":
			x.wrapperCalls(recv+"."+prop, args, scope, line)
		default:
			if !strings.HasPrefix(prop, "use") {
				x.wrapperCalls(recv+"."+prop, args, scope, line)
			}
		}
	}
}

// jsLooksLikeRoute reports `x.get('/path', handler)` on a receiver this file
// did not create: a path literal followed by at least one handler.
func jsLooksLikeRoute(b *builder, prop string, args []*tree_sitter.Node) bool {
	if !(isHTTPVerb(prop) || prop == "all" || prop == "use" || prop == "route") || len(args) < 2 {
		return false
	}
	if args[0].Kind() != "string" && args[0].Kind() != "template_string" {
		return false
	}
	switch args[len(args)-1].Kind() {
	case "arrow_function", "function_expression", "function", "identifier", "member_expression", "call_expression":
		return true
	}
	return false
}

func jsHandlerName(b *builder, args []*tree_sitter.Node) string {
	if len(args) == 0 {
		return ""
	}
	last := args[len(args)-1]
	switch last.Kind() {
	case "identifier":
		return b.text(last)
	case "member_expression":
		return b.text(field(last, "property"))
	}
	return ""
}

func (x *jsHTTP) routeOrMount(recv, prop string, args []*tree_sitter.Node, line int, scope *jsScope) {
	if len(args) == 0 {
		return
	}
	switch {
	case prop == "use" || prop == "route":
		prefix := ""
		rest := args
		if args[0].Kind() == "string" || args[0].Kind() == "template_string" || args[0].Kind() == "identifier" && x.consts[x.b.text(args[0])] != nil {
			v := x.eval(args[0], scope, 0)
			if _, _, param := splitURL(v); param >= 0 {
				return // a helper's own body; expanded at its call sites
			}
			prefix = v.render()
			rest = args[1:]
		}
		for _, a := range rest {
			x.mountTarget(recv, prefix, a)
		}
	case isHTTPVerb(prop) || prop == "all":
		v := x.eval(args[0], scope, 0)
		p := v.render()
		if !strings.HasPrefix(p, "/") && p != "*" {
			return
		}
		method := strings.ToUpper(prop)
		if prop == "all" {
			method = AnyMethod
		}
		x.rec.route(method, p, recv, jsHandlerName(x.b, args[1:]), line)
	}
}

func (x *jsHTTP) mountTarget(recv, prefix string, a *tree_sitter.Node) {
	switch a.Kind() {
	case "identifier":
		name := x.b.text(a)
		if x.routers[name] != "" {
			x.rec.mount(prefix, recv, name, nil)
		} else if spec := x.bindings[name]; spec != "" {
			x.rec.mount(prefix, recv, x.named[name], []string{spec})
		}
	case "member_expression":
		// app.use('/x', routes.users) where routes = require('./routes')
		if spec := x.bindings[x.b.text(field(a, "object"))]; spec != "" {
			x.rec.mount(prefix, recv, x.b.text(field(a, "property")), []string{spec})
		}
	case "call_expression":
		if spec := requireSpec(x.b, a); spec != "" {
			x.rec.mount(prefix, recv, "", []string{spec})
		}
	}
}

func (x *jsHTTP) expandHelper(h *mountHelper, args []*tree_sitter.Node, scope *jsScope) {
	for _, m := range h.mounts {
		v := x.evalWith(m.prefix, func(name string) (strVal, bool) {
			if i, ok := h.params[name]; ok && i < len(args) {
				return x.eval(args[i], scope, 0), true
			}
			return nil, false
		})
		prefix := v.render()
		for _, a := range args {
			if a.Kind() == "identifier" || a.Kind() == "member_expression" || a.Kind() == "call_expression" {
				x.mountTarget(m.router, prefix, a)
			}
		}
	}
}

func (x *jsHTTP) libCall(recv, prop string, args []*tree_sitter.Node, scope *jsScope, line int) {
	base := x.instances[recv]
	switch {
	case isHTTPVerb(prop):
		if len(args) > 0 {
			x.rec.direct(strings.ToUpper(prop), x.eval(args[0], scope, 0), base, line, scope.node())
		}
	case prop == "request":
		x.axiosConfigCall(args, scope, line)
	}
}

// axiosConfigCall handles axios(url, config), axios({url, method}) and
// client.request({url, method}).
func (x *jsHTTP) axiosConfigCall(args []*tree_sitter.Node, scope *jsScope, line int) {
	if len(args) == 0 {
		return
	}
	if args[0].Kind() != "object" {
		x.rec.direct(jsMethodOption(x.b, args, 1, "GET"), x.eval(args[0], scope, 0), nil, line, scope.node())
		return
	}
	var url strVal
	var base []string
	method := "GET"
	for i := uint(0); i < args[0].NamedChildCount(); i++ {
		p := args[0].NamedChild(i)
		if p == nil || p.Kind() != "pair" {
			continue
		}
		switch strings.Trim(x.b.text(field(p, "key")), `"'`) {
		case "url":
			url = x.eval(field(p, "value"), scope, 0)
		case "baseURL":
			base = x.eval(field(p, "value"), scope, 0).tokens()
		case "method":
			if m, ok := x.eval(field(p, "value"), scope, 0).literal(); ok {
				method = strings.ToUpper(m)
			} else {
				method = AnyMethod
			}
		}
	}
	if url != nil {
		x.rec.direct(method, url, base, line, scope.node())
	}
}

// jsMethodOption reads `method` from the options object at args[i].
func jsMethodOption(b *builder, args []*tree_sitter.Node, i int, def string) string {
	if i >= len(args) || args[i].Kind() != "object" {
		return def
	}
	for j := uint(0); j < args[i].NamedChildCount(); j++ {
		p := args[i].NamedChild(j)
		if p != nil && p.Kind() == "shorthand_property_identifier" && b.text(p) == "method" {
			return AnyMethod // { method } passed through from the caller
		}
		if p != nil && p.Kind() == "pair" && strings.Trim(b.text(field(p, "key")), `"'`) == "method" {
			if v := field(p, "value"); v != nil && v.Kind() == "string" {
				return strings.ToUpper(unquote(b.text(v)))
			}
			return AnyMethod
		}
	}
	return def
}

// wrapperCalls records each path-like argument of a call to a function that
// may wrap an HTTP client: apiGet('/x'), this.request('GET', path).
func (x *jsHTTP) wrapperCalls(callee string, args []*tree_sitter.Node, scope *jsScope, line int) {
	if len(args) > 1 {
		switch args[len(args)-1].Kind() {
		case "arrow_function", "function_expression", "function":
			return // a callback-taking call (a route, an event handler), not a request
		}
	}
	if notRequestMethod(callee) {
		return
	}
	method := verbHint(callee)
	for _, a := range args {
		if a.Kind() == "string" && method == AnyMethod {
			if m := strings.ToLower(unquote(x.b.text(a))); isHTTPVerb(m) {
				method = strings.ToUpper(m)
			}
		}
	}
	for i, a := range args {
		if i > 2 {
			break
		}
		switch a.Kind() {
		case "string", "template_string", "binary_expression", "identifier", "member_expression", "call_expression":
		default:
			continue
		}
		v := x.eval(a, scope, 0)
		if s, ok := v.literal(); ok && !strings.HasPrefix(s, "/") {
			continue
		}
		x.rec.wrapperCall(callee, i, method, v, line, scope.node())
	}
}

// eval statically evaluates a string expression.
func (x *jsHTTP) eval(n *tree_sitter.Node, scope *jsScope, depth int) strVal {
	return x.evalScoped(n, scope, depth, nil)
}

// evalWith evaluates n with identifiers first looked up in sub.
func (x *jsHTTP) evalWith(n *tree_sitter.Node, sub func(string) (strVal, bool)) strVal {
	return x.evalScoped(n, nil, 0, sub)
}

func (x *jsHTTP) evalScoped(n *tree_sitter.Node, scope *jsScope, depth int, sub func(string) (strVal, bool)) strVal {
	if n == nil || depth > 6 {
		return dynVal()
	}
	rec := func(c *tree_sitter.Node) strVal { return x.evalScoped(c, scope, depth+1, sub) }
	switch n.Kind() {
	case "string":
		return litVal(unquote(x.b.text(n)))
	case "template_string":
		var out strVal
		for i := uint(0); i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			switch c.Kind() {
			case "string_fragment", "escape_sequence":
				out = append(out, litVal(x.b.text(c))...)
			case "template_substitution":
				if c.NamedChildCount() > 0 {
					out = append(out, rec(c.NamedChild(0))...)
				}
			}
		}
		return out
	case "binary_expression":
		op := x.b.text(n.ChildByFieldName("operator"))
		l, r := rec(field(n, "left")), rec(field(n, "right"))
		switch op {
		case "+":
			return append(append(strVal{}, l...), r...)
		case "||", "??":
			if _, ok := l.literal(); ok {
				return l
			}
			return dynVal(append(l.tokens(), r.tokens()...)...)
		}
		return dynVal()
	case "ternary_expression":
		return dynVal(append(rec(field(n, "consequence")).tokens(), rec(field(n, "alternative")).tokens()...)...)
	case "parenthesized_expression", "await_expression", "as_expression", "non_null_expression", "satisfies_expression":
		if n.NamedChildCount() > 0 {
			return rec(n.NamedChild(0))
		}
	case "identifier":
		name := x.b.text(n)
		if sub != nil {
			if v, ok := sub(name); ok {
				return v
			}
		}
		for s := scope; s != nil; s = s.parent {
			if v, ok := s.consts[name]; ok {
				if v == nil {
					return dynVal(name)
				}
				return rec(v)
			}
			if i, ok := s.params[name]; ok {
				if s == scope {
					return paramVal(i)
				}
				return dynVal() // an outer function's parameter
			}
		}
		if v, ok := x.consts[name]; ok {
			return rec(v)
		}
		return dynVal(name)
	case "member_expression", "subscript_expression":
		if env := envName(x.b, n); env != "" {
			return dynVal(env)
		}
		if n.Kind() == "member_expression" {
			obj := x.b.text(field(n, "object"))
			prop := x.b.text(field(n, "property"))
			if obj == "this" {
				if v, ok := x.consts[prop]; ok {
					return rec(v)
				}
			}
			return dynVal(append(identTokens(x.b, field(n, "object")), prop)...)
		}
		return dynVal(identTokens(x.b, n)...)
	case "call_expression":
		fn := field(n, "function")
		args := callArgs(n)
		if fn != nil && fn.Kind() == "member_expression" {
			prop := x.b.text(field(fn, "property"))
			switch prop {
			case "replace", "replaceAll", "trim", "toString", "trimEnd", "trimStart", "concat", "slice":
				// A normalized base: keep what it was built from.
				obj := rec(field(fn, "object"))
				if s, ok := obj.literal(); ok {
					return litVal(s)
				}
				return dynVal(obj.tokens()...)
			}
		}
		name := x.b.text(fn)
		// A URL helper passing the caller's path parameter through
		// (resolveUrl(path), buildUrl(base, path)) still carries it.
		lname := strings.ToLower(name)
		if fn != nil && (strings.Contains(lname, "url") || strings.Contains(lname, "path")) && !strings.Contains(lname, "uricomponent") {
			var out strVal
			for _, a := range args {
				v := rec(a)
				out = append(out, v...)
			}
			if len(out) > 0 {
				return append(dynVal(x.funcEnv[name]...), out...)
			}
		}
		toks := append([]string{}, x.funcEnv[name]...)
		if fn != nil && fn.Kind() == "member_expression" {
			toks = append(toks, identTokens(x.b, field(fn, "object"))...)
		} else {
			toks = append(toks, name)
		}
		return dynVal(toks...)
	case "new_expression":
		if x.b.text(field(n, "constructor")) == "URL" {
			args := callArgs(n)
			if len(args) >= 2 {
				return append(dynVal(rec(args[1]).tokens()...), rec(args[0])...)
			}
			if len(args) == 1 {
				return rec(args[0])
			}
		}
	}
	return dynVal()
}

// identTokens are the identifier and property names in an expression.
func identTokens(b *builder, n *tree_sitter.Node) []string {
	var out []string
	walkNamed(n, func(c *tree_sitter.Node) {
		switch c.Kind() {
		case "identifier", "property_identifier", "shorthand_property_identifier":
			out = append(out, b.text(c))
		}
	})
	return out
}

// recordNextRoute records the HTTP methods a Next.js route handler file
// exports (export async function GET / export const POST = ...). Whether the
// file is a route file is decided from its path at index time.
func (x *jsHTTP) recordNextRoute(root *tree_sitter.Node) {
	for i := uint(0); i < root.NamedChildCount(); i++ {
		c := root.NamedChild(i)
		if c == nil || c.Kind() != "export_statement" {
			continue
		}
		d := field(c, "declaration")
		if d == nil {
			if c.NamedChildCount() > 0 && strings.Contains(x.b.text(c), "export default") {
				x.b.addListProp(x.rec.module, PropNextRoute, "default")
			}
			continue
		}
		var names []string
		switch d.Kind() {
		case "function_declaration":
			names = append(names, x.b.text(field(d, "name")))
		case "lexical_declaration":
			for j := uint(0); j < d.NamedChildCount(); j++ {
				if v := d.NamedChild(j); v != nil && v.Kind() == "variable_declarator" {
					names = append(names, x.b.text(field(v, "name")))
				}
			}
		}
		for _, name := range names {
			if isHTTPVerb(strings.ToLower(name)) && name == strings.ToUpper(name) {
				x.b.addListProp(x.rec.module, PropNextRoute, name)
			}
		}
	}
}

// isEnvStyleName reports an upper-snake-case name with an underscore
// (BACKEND_URL), the shape of an environment variable.
func isEnvStyleName(s string) bool {
	return strings.Contains(s, "_") && isTokenWord(s) && s == strings.ToUpper(s)
}
