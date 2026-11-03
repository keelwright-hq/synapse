package parse

import (
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// extractHTTPPython records FastAPI/Flask routes, router prefixes and
// include_router mounts, httpx/requests/aiohttp client calls (and functions
// wrapping them), environment reads and pydantic settings field aliases.
func extractHTTPPython(b *builder, root *tree_sitter.Node, module graph.NodeID) {
	x := &pyHTTP{
		rec:       newHTTPRecorder(b, module),
		b:         b,
		consts:    map[string]*tree_sitter.Node{},
		bindings:  map[string]pyBinding{},
		routers:   map[string]string{},
		kinds:     map[string]string{},
		instances: map[string][]string{},
	}
	x.prepass(root)
	x.walk(root, nil)
	for name, prefix := range x.routers {
		x.rec.router(name, prefix, false, x.kinds[name])
	}
}

type pyHTTP struct {
	rec    *httpRecorder
	b      *builder
	consts map[string]*tree_sitter.Node
	// bindings map an imported local name to the modules it may come from
	// (`from a.b import c` -> a.b.c or a.b) and its original name.
	bindings map[string]pyBinding
	// routers are APIRouter/FastAPI/Blueprint/Flask objects and their prefix.
	routers map[string]string
	kinds   map[string]string
	// instances are HTTP clients created with a base URL, by name (self._http).
	instances map[string][]string
}

type pyBinding struct {
	specs []string
	orig  string
}

type pyScope struct {
	fn     *tree_sitter.Node
	params map[string]int
	consts map[string]*tree_sitter.Node
}

func (s *pyScope) node() *tree_sitter.Node {
	if s == nil {
		return nil
	}
	return s.fn
}

var pyRouterFactories = map[string]bool{
	"APIRouter": true, "FastAPI": true, "Blueprint": true, "Flask": true, "fastapi.APIRouter": true,
	"fastapi.FastAPI": true, "flask.Blueprint": true, "flask.Flask": true,
}

var pyClientFactories = map[string]bool{
	"httpx.Client": true, "httpx.AsyncClient": true, "aiohttp.ClientSession": true,
	"AsyncClient": true, "ClientSession": true, "requests.Session": true,
}

var pyHTTPLibs = map[string]bool{"httpx": true, "requests": true}

func (x *pyHTTP) prepass(root *tree_sitter.Node) {
	walkNamed(root, func(n *tree_sitter.Node) {
		switch n.Kind() {
		case "import_from_statement":
			mod := x.b.text(field(n, "module_name"))
			for i := uint(0); i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				if c == nil || sameNode(c, field(n, "module_name")) {
					continue
				}
				orig, local := c, c
				if c.Kind() == "aliased_import" {
					orig, local = field(c, "name"), field(c, "alias")
				}
				if orig == nil || local == nil {
					continue
				}
				o := x.b.text(orig)
				specs := []string{mod + "." + o, mod}
				x.bindings[x.b.text(local)] = pyBinding{specs: specs, orig: o}
				if looksLikeRouterName(x.b.text(local)) && !pyRouterFactories[o] {
					x.b.addListProp(x.rec.module, PropHTTPImports, EncodeRecord(x.b.text(local), o, strings.Join(specs, ";")))
				}
			}
		case "import_statement":
			for i := uint(0); i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				if c == nil {
					continue
				}
				if c.Kind() == "aliased_import" {
					x.bindings[x.b.text(field(c, "alias"))] = pyBinding{specs: []string{x.b.text(field(c, "name"))}}
				} else {
					x.bindings[x.b.text(c)] = pyBinding{specs: []string{x.b.text(c)}}
				}
			}
		case "assignment":
			left, right := field(n, "left"), field(n, "right")
			if left == nil || right == nil {
				return
			}
			name := x.b.text(left)
			if right.Kind() == "call" {
				fn := x.b.text(field(right, "function"))
				switch {
				case pyRouterFactories[fn]:
					x.routers[name] = x.kwargString(right, "prefix", "url_prefix")
					x.kinds[name] = "app"
					if strings.HasSuffix(fn, "APIRouter") || strings.HasSuffix(fn, "Blueprint") {
						x.kinds[name] = "router"
					}
					return
				case pyClientFactories[fn]:
					x.instances[name] = cleanTokens(x.kwargTokens(right, "base_url"))
					return
				}
			}
			if left.Kind() == "identifier" && pyIsConstScope(n) && isPyStringish(right) {
				x.consts[name] = right
			}
		case "with_item":
			// async with httpx.AsyncClient(base_url=X) as client:
			v := field(n, "value")
			if v != nil && v.Kind() == "as_pattern" && v.NamedChildCount() >= 2 {
				call := v.NamedChild(0)
				alias := field(v, "alias")
				if call != nil && call.Kind() == "call" && alias != nil && pyClientFactories[x.b.text(field(call, "function"))] {
					x.instances[x.b.text(alias)] = cleanTokens(x.kwargTokens(call, "base_url"))
				}
			}
		case "class_definition":
			x.settingsClass(n)
		}
	})
}

// pyIsConstScope reports an assignment at module or class level.
func pyIsConstScope(n *tree_sitter.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "function_definition":
			return false
		case "module", "class_definition":
			return true
		}
	}
	return true
}

func isPyStringish(v *tree_sitter.Node) bool {
	switch v.Kind() {
	case "string", "concatenated_string", "binary_operator", "attribute", "call", "parenthesized_expression", "subscript":
		return true
	}
	return false
}

// settingsClass records the aliases of a pydantic BaseSettings subclass's
// fields (a field loads from the env var of its name, upper-cased, or from
// its aliases). Declaring a field is not reading it: the env read is
// recorded where code uses `settings.field`.
func (x *pyHTTP) settingsClass(n *tree_sitter.Node) {
	if !strings.Contains(x.b.text(field(n, "superclasses")), "BaseSettings") {
		return
	}
	body := field(n, "body")
	if body == nil {
		return
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		st := body.NamedChild(i)
		if st == nil || st.Kind() != "expression_statement" || st.NamedChildCount() == 0 {
			continue
		}
		as := st.NamedChild(0)
		if as.Kind() != "assignment" {
			continue
		}
		left := field(as, "left")
		if left == nil || left.Kind() != "identifier" {
			continue
		}
		fieldName := strings.ToUpper(x.b.text(left))
		right := field(as, "right")
		if right == nil {
			continue
		}
		walkNamed(right, func(c *tree_sitter.Node) {
			if c.Kind() != "keyword_argument" {
				return
			}
			switch x.b.text(field(c, "name")) {
			case "alias", "validation_alias", "env":
				walkNamed(field(c, "value"), func(s *tree_sitter.Node) {
					if s.Kind() == "string" {
						alias := strings.ToUpper(pyStringText(x.b, s))
						if alias != fieldName {
							x.b.addListProp(x.rec.module, PropEnvAliases, fieldName+"="+alias)
						}
					}
				})
			}
		})
	}
}

func pyStringText(b *builder, s *tree_sitter.Node) string {
	var sb strings.Builder
	for i := uint(0); i < s.NamedChildCount(); i++ {
		if c := s.NamedChild(i); c != nil && c.Kind() == "string_content" {
			sb.WriteString(b.text(c))
		}
	}
	return sb.String()
}

func (x *pyHTTP) kwarg(call *tree_sitter.Node, names ...string) *tree_sitter.Node {
	args := field(call, "arguments")
	if args == nil {
		return nil
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		a := args.NamedChild(i)
		if a == nil || a.Kind() != "keyword_argument" {
			continue
		}
		k := x.b.text(field(a, "name"))
		for _, n := range names {
			if k == n {
				return field(a, "value")
			}
		}
	}
	return nil
}

func (x *pyHTTP) kwargString(call *tree_sitter.Node, names ...string) string {
	v := x.kwarg(call, names...)
	if v == nil {
		return ""
	}
	return x.eval(v, nil, 0).render()
}

func (x *pyHTTP) kwargTokens(call *tree_sitter.Node, names ...string) []string {
	v := x.kwarg(call, names...)
	if v == nil {
		return nil
	}
	return x.eval(v, nil, 0).tokens()
}

func pyPositional(call *tree_sitter.Node) []*tree_sitter.Node {
	args := field(call, "arguments")
	if args == nil {
		return nil
	}
	var out []*tree_sitter.Node
	for i := uint(0); i < args.NamedChildCount(); i++ {
		a := args.NamedChild(i)
		if a == nil || a.Kind() == "keyword_argument" || a.Kind() == "comment" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func pyParams(b *builder, fn *tree_sitter.Node) map[string]int {
	out := map[string]int{}
	ps := field(fn, "parameters")
	if ps == nil {
		return out
	}
	idx := 0
	for i := uint(0); i < ps.NamedChildCount(); i++ {
		p := ps.NamedChild(i)
		if p == nil {
			continue
		}
		name := p
		switch p.Kind() {
		case "typed_parameter":
			name = p.NamedChild(0)
		case "default_parameter", "typed_default_parameter":
			name = field(p, "name")
		case "identifier":
		default:
			continue
		}
		if name == nil {
			continue
		}
		n := b.text(name)
		if n == "self" || n == "cls" {
			continue
		}
		out[n] = idx
		idx++
	}
	return out
}

func (x *pyHTTP) walk(n *tree_sitter.Node, scope *pyScope) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "function_definition":
		scope = &pyScope{fn: n, params: pyParams(x.b, n), consts: map[string]*tree_sitter.Node{}}
	case "decorated_definition":
		x.decorated(n)
	case "decorator":
		return // route decorators are not client calls
	case "assignment":
		if scope != nil {
			if left, right := field(n, "left"), field(n, "right"); left != nil && right != nil && left.Kind() == "identifier" {
				scope.consts[x.b.text(left)] = right
			}
		}
	case "attribute":
		// settings.backend_url / config.API_URL: a settings field read.
		obj := strings.ToLower(x.b.text(field(n, "object")))
		if strings.HasSuffix(obj, "settings") || strings.HasSuffix(obj, "config") {
			x.rec.envRead(x.b.text(field(n, "attribute")), scope.node(), lineOf(n))
		}
	case "subscript", "call":
		if v := pyEnvName(x.b, n); v != "" {
			x.rec.envRead(v, scope.node(), lineOf(n))
		}
		if n.Kind() == "call" {
			x.call(n, scope)
		}
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		x.walk(n.NamedChild(i), scope)
	}
}

// pyEnvName returns X for os.environ["X"], os.environ.get("X") and
// os.getenv("X").
func pyEnvName(b *builder, n *tree_sitter.Node) string {
	switch n.Kind() {
	case "subscript":
		if strings.HasSuffix(b.text(field(n, "value")), "environ") {
			if s := field(n, "subscript"); s != nil && s.Kind() == "string" {
				return pyStringText(b, s)
			}
		}
	case "call":
		fn := b.text(field(n, "function"))
		if fn == "os.getenv" || fn == "getenv" || strings.HasSuffix(fn, "environ.get") {
			if args := pyPositional(n); len(args) > 0 && args[0].Kind() == "string" {
				return pyStringText(b, args[0])
			}
		}
	}
	return ""
}

// decorated records `@router.get("/x")` / `@app.route("/x", methods=[...])`.
func (x *pyHTTP) decorated(n *tree_sitter.Node) {
	def := field(n, "definition")
	if def == nil || def.Kind() != "function_definition" {
		return
	}
	handler := x.b.text(field(def, "name"))
	for i := uint(0); i < n.NamedChildCount(); i++ {
		d := n.NamedChild(i)
		if d == nil || d.Kind() != "decorator" || d.NamedChildCount() == 0 {
			continue
		}
		call := d.NamedChild(0)
		if call.Kind() != "call" {
			continue
		}
		fn := field(call, "function")
		if fn == nil || fn.Kind() != "attribute" {
			continue
		}
		recv := x.b.text(field(fn, "object"))
		attr := x.b.text(field(fn, "attribute"))
		args := pyPositional(call)
		if len(args) == 0 {
			continue
		}
		path := x.eval(args[0], nil, 0).render()
		if !strings.HasPrefix(path, "/") && path != "" {
			continue
		}
		var methods []string
		switch {
		case isHTTPVerb(attr):
			methods = []string{strings.ToUpper(attr)}
		case attr == "route" || attr == "api_route":
			if m := x.kwarg(call, "methods"); m != nil {
				walkNamed(m, func(s *tree_sitter.Node) {
					if s.Kind() == "string" {
						methods = append(methods, strings.ToUpper(pyStringText(x.b, s)))
					}
				})
			}
			if len(methods) == 0 {
				methods = []string{AnyMethod}
				if attr == "route" {
					methods = []string{"GET"} // Flask's default
				}
			}
		default:
			continue
		}
		for _, m := range methods {
			x.rec.route(m, path, recv, handler, lineOf(def))
		}
	}
}

func (x *pyHTTP) call(n *tree_sitter.Node, scope *pyScope) {
	fn := field(n, "function")
	if fn == nil {
		return
	}
	args := pyPositional(n)
	line := lineOf(n)
	if fn.Kind() == "attribute" {
		recv := x.b.text(field(fn, "object"))
		attr := x.b.text(field(fn, "attribute"))
		switch {
		case attr == "include_router" || attr == "register_blueprint":
			if len(args) > 0 {
				prefix := x.kwargString(n, "prefix", "url_prefix")
				x.mount(recv, prefix, args[0])
			}
			return
		case pyHTTPLibs[recv] || x.instances[recv] != nil:
			base := x.instances[recv]
			switch {
			case isHTTPVerb(attr) && len(args) > 0:
				x.rec.direct(strings.ToUpper(attr), x.eval(args[0], scope, 0), base, line, scope.node())
			case (attr == "request" || attr == "stream") && len(args) > 1:
				method := AnyMethod
				if m, ok := x.eval(args[0], scope, 0).literal(); ok {
					method = strings.ToUpper(m)
				}
				x.rec.direct(method, x.eval(args[1], scope, 0), base, line, scope.node())
			}
			return
		}
		x.wrapperCalls(recv+"."+attr, args, scope, line)
		return
	}
	x.wrapperCalls(x.b.text(fn), args, scope, line)
}

func (x *pyHTTP) mount(recv, prefix string, target *tree_sitter.Node) {
	switch target.Kind() {
	case "identifier":
		name := x.b.text(target)
		if _, ok := x.routers[name]; ok {
			x.rec.mount(prefix, recv, name, nil)
		} else if bnd, ok := x.bindings[name]; ok {
			x.rec.mount(prefix, recv, bnd.orig, bnd.specs)
		}
	case "attribute":
		// app.include_router(chat.router) with `from app.routers import chat`
		obj := x.b.text(field(target, "object"))
		if bnd, ok := x.bindings[obj]; ok {
			x.rec.mount(prefix, recv, x.b.text(field(target, "attribute")), bnd.specs)
		}
	}
}

func (x *pyHTTP) wrapperCalls(callee string, args []*tree_sitter.Node, scope *pyScope, line int) {
	if notRequestMethod(callee) {
		return
	}
	method := verbHint(callee)
	for _, a := range args {
		if a.Kind() == "string" && method == AnyMethod {
			if m := strings.ToLower(pyStringText(x.b, a)); isHTTPVerb(m) {
				method = strings.ToUpper(m)
			}
		}
	}
	for i, a := range args {
		if i > 2 {
			break
		}
		switch a.Kind() {
		case "string", "concatenated_string", "binary_operator", "identifier", "attribute", "call":
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

func (x *pyHTTP) eval(n *tree_sitter.Node, scope *pyScope, depth int) strVal {
	if n == nil || depth > 6 {
		return dynVal()
	}
	rec := func(c *tree_sitter.Node) strVal { return x.eval(c, scope, depth+1) }
	switch n.Kind() {
	case "string":
		var out strVal
		for i := uint(0); i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			switch c.Kind() {
			case "string_content", "escape_sequence":
				out = append(out, litVal(x.b.text(c))...)
			case "interpolation":
				out = append(out, rec(field(c, "expression"))...)
			}
		}
		if len(out) == 0 {
			return litVal("")
		}
		return out
	case "concatenated_string":
		var out strVal
		for i := uint(0); i < n.NamedChildCount(); i++ {
			out = append(out, rec(n.NamedChild(i))...)
		}
		return out
	case "binary_operator":
		if x.b.text(n.ChildByFieldName("operator")) == "+" {
			return append(append(strVal{}, rec(field(n, "left"))...), rec(field(n, "right"))...)
		}
	case "boolean_operator":
		l := rec(field(n, "left"))
		if _, ok := l.literal(); ok {
			return l
		}
		return dynVal(append(l.tokens(), rec(field(n, "right")).tokens()...)...)
	case "parenthesized_expression", "await":
		if n.NamedChildCount() > 0 {
			return rec(n.NamedChild(0))
		}
	case "identifier":
		name := x.b.text(n)
		if scope != nil {
			if v, ok := scope.consts[name]; ok {
				return rec(v)
			}
			if i, ok := scope.params[name]; ok {
				return paramVal(i)
			}
		}
		if v, ok := x.consts[name]; ok {
			return rec(v)
		}
		return dynVal(name)
	case "attribute":
		obj := x.b.text(field(n, "object"))
		attr := x.b.text(field(n, "attribute"))
		if obj == "self" || obj == "cls" || x.consts[attr] != nil {
			if v, ok := x.consts[attr]; ok {
				return rec(v)
			}
		}
		return dynVal(append(pyIdentTokens(x.b, field(n, "object")), attr)...)
	case "subscript", "call":
		if env := pyEnvName(x.b, n); env != "" {
			return dynVal(env)
		}
		if n.Kind() == "call" {
			fn := field(n, "function")
			name := x.b.text(fn)
			args := pyPositional(n)
			switch {
			case name == "str" && len(args) == 1:
				return rec(args[0])
			case strings.HasSuffix(name, "urljoin") && len(args) == 2:
				return append(dynVal(rec(args[0]).tokens()...), rec(args[1])...)
			case fn != nil && fn.Kind() == "attribute" && (x.b.text(field(fn, "attribute")) == "rstrip" || x.b.text(field(fn, "attribute")) == "strip"):
				return dynVal(rec(field(fn, "object")).tokens()...)
			}
			return dynVal(pyIdentTokens(x.b, fn)...)
		}
		return dynVal(pyIdentTokens(x.b, n)...)
	}
	return dynVal()
}

func pyIdentTokens(b *builder, n *tree_sitter.Node) []string {
	var out []string
	walkNamed(n, func(c *tree_sitter.Node) {
		if c.Kind() == "identifier" {
			if t := b.text(c); t != "self" && t != "cls" {
				out = append(out, t)
			}
		}
	})
	return out
}

// looksLikeRouterName reports an imported name worth following when a
// package re-exports routers (`from .users import router as users_router`).
func looksLikeRouterName(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "router") || strings.Contains(l, "blueprint") || l == "bp" || strings.HasSuffix(l, "_bp")
}
