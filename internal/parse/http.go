package parse

import (
	"sort"
	"strconv"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// HTTP records are list props written at parse time and resolved at index
// time into route operations and client call sites (see internal/index
// resolve_http.go). Each value is one record: fields joined by '|', with ','
// and '|' escaped so records survive the comma-separated list encoding.
const (
	// PropHTTPRoutes (module): METHOD|path|router|handler|line.
	PropHTTPRoutes = "http_routes"
	// PropHTTPRouters (module): var|prefix|exported|kind. A router or app
	// created in this file, with its own prefix (APIRouter(prefix=...)); kind
	// "router" is only reachable where something mounts it.
	PropHTTPRouters = "http_routers"
	// PropHTTPMounts (module): prefix|router|target|specs. `router` mounts
	// `target` (a local router, or one imported from one of specs) at prefix.
	PropHTTPMounts = "http_mounts"
	// PropHTTPCalls (function, method or module): kind|callee|arg|METHOD|
	// tokens|tail|param|line. kind "direct" is a call to an HTTP client;
	// "wrapper" is a call to a function that may wrap one, passing a path in
	// argument arg (a tail, or the caller's own parameter param).
	PropHTTPCalls = "http_calls"
	// PropHTTPWrapper (function or method): arg|METHOD|tokens. The function
	// sends an HTTP request to a path taken from its parameter arg.
	PropHTTPWrapper = "http_wrapper"
	// PropHTTPClients (function, method or module, written by the index): the
	// resolved client calls, METHOD|tokens|tail|line.
	PropHTTPClients = "http_clients"
	// PropHTTPImports (module): local|orig|specs. An imported name that may be
	// a router re-exported to the file that mounts it (a package __init__).
	PropHTTPImports = "http_imports"
	// PropNextRoute (module): HTTP methods exported by a Next.js route file.
	PropNextRoute = "next_route_methods"
	// PropEnvReads (module): environment variable names the file reads.
	PropEnvReads = "env_reads"
	// PropEnvAliases (module): FIELD=ALIAS pairs from settings classes whose
	// fields load from differently named environment variables.
	PropEnvAliases = "env_aliases"
)

// AnyMethod matches every HTTP method (Express `all`, an unknown method).
const AnyMethod = "*"

var recordEscaper = strings.NewReplacer("%", "%25", ",", "%2C", "|", "%7C")
var recordUnescaper = strings.NewReplacer("%2C", ",", "%7C", "|", "%25", "%")

// EncodeRecord joins fields into one list-prop value.
func EncodeRecord(fields ...string) string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = recordEscaper.Replace(f)
	}
	return strings.Join(out, "|")
}

// DecodeRecords splits a list prop into its records' fields.
func DecodeRecords(prop string) [][]string {
	if prop == "" {
		return nil
	}
	var out [][]string
	for _, rec := range strings.Split(prop, ",") {
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "|")
		for i, f := range fields {
			fields[i] = recordUnescaper.Replace(f)
		}
		out = append(out, fields)
	}
	return out
}

// JoinTokens and SplitTokens encode a token set inside one record field.
func JoinTokens(tokens []string) string { return strings.Join(tokens, ";") }

func SplitTokens(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ";")
}

// NormalizeHTTPPath turns a route or client path into the form routes are
// matched on: no query or fragment, no empty or trailing segments, and every
// parameter (:id, {id}, [id], an interpolated value) written as {}.
func NormalizeHTTPPath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	var segs []string
	for _, s := range strings.Split(p, "/") {
		switch {
		case s == "":
			continue
		case strings.HasPrefix(s, ":"),
			strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"),
			strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"),
			strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">"), // Flask <int:id>
			strings.Contains(s, "{}"):
			s = "{}"
		}
		segs = append(segs, s)
	}
	return "/" + strings.Join(segs, "/")
}

// isHTTPVerb reports a lowercase HTTP method name used as a client or router
// method (get, post, ...).
func isHTTPVerb(s string) bool {
	switch s {
	case "get", "post", "put", "patch", "delete", "head", "options":
		return true
	}
	return false
}

// verbHint guesses the method of a wrapper call from its name: apiGet,
// get_json, _post -> GET, GET, POST. Unknown is AnyMethod.
func verbHint(name string) string {
	l := strings.ToLower(name)
	if i := strings.LastIndex(l, "."); i >= 0 {
		l = l[i+1:]
	}
	for _, v := range []string{"delete", "patch", "post", "put", "get"} {
		if strings.HasPrefix(l, v) || strings.HasSuffix(l, v) || strings.HasPrefix(strings.TrimLeft(l, "_"), v) {
			return strings.ToUpper(v)
		}
	}
	return AnyMethod
}

// strPiece is one part of a statically evaluated string: literal text, or a
// dynamic value with the identifiers it was built from (tokens) or the
// enclosing function's parameter it came from (param >= 0).
type strPiece struct {
	lit    string
	dyn    bool
	tokens []string
	param  int
}

type strVal []strPiece

func litVal(s string) strVal { return strVal{{lit: s, param: -1}} }

func dynVal(tokens ...string) strVal {
	return strVal{{dyn: true, tokens: tokens, param: -1}}
}

func paramVal(i int) strVal { return strVal{{dyn: true, param: i}} }

func (v strVal) tokens() []string {
	var out []string
	for _, p := range v {
		out = append(out, p.tokens...)
	}
	return out
}

// literal returns the value when it is fully static.
func (v strVal) literal() (string, bool) {
	var sb strings.Builder
	for _, p := range v {
		if p.dyn {
			return "", false
		}
		sb.WriteString(p.lit)
	}
	return sb.String(), true
}

// render writes dynamic pieces as {} (a path parameter).
func (v strVal) render() string {
	var sb strings.Builder
	for _, p := range v {
		if p.dyn {
			sb.WriteString("{}")
		} else {
			sb.WriteString(p.lit)
		}
	}
	return sb.String()
}

// splitURL reads a client URL as base + tail. Leading dynamic pieces are the
// base (an env var, a config field, a normalizer call) and their tokens name
// it; the rest, from the first literal, is the path tail. A leading parameter
// means the URL is passed in (param >= 0, no tail): the function is a wrapper.
func splitURL(v strVal) (tokens []string, tail string, param int) {
	param = -1
	i := 0
	for i < len(v) && v[i].dyn && v[i].param < 0 {
		tokens = append(tokens, v[i].tokens...)
		i++
	}
	if i < len(v) && v[i].dyn && v[i].param >= 0 {
		for _, p := range v[i+1:] {
			if !p.dyn && strings.TrimSpace(p.lit) != "" && !strings.HasPrefix(p.lit, "?") {
				// `${base}${path}/x`: more path after the parameter.
				return tokens, "", -1
			}
		}
		return tokens, "", v[i].param
	}
	rest := strVal(v[i:]).render()
	if i == 0 {
		switch {
		case strings.HasPrefix(rest, "http://"), strings.HasPrefix(rest, "https://"):
			rest = strings.SplitN(rest, "://", 2)[1]
			slash := strings.Index(rest, "/")
			if slash < 0 {
				return tokens, "", -1
			}
			rest = rest[slash:]
		case !strings.HasPrefix(rest, "/"):
			return tokens, "", -1
		}
	} else if rest != "" && !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "?") {
		rest = "/" + rest
	}
	tail = NormalizeHTTPPath(rest)
	if !hasLiteralSegment(tail) {
		return tokens, "", -1 // `${a}/${b}` is a key, not a route path
	}
	return tokens, tail, -1
}

// cleanTokens upper-cases identifier tokens (env names compare that way;
// pydantic loads backend_base_url from BACKEND_BASE_URL), drops noise and
// sorts them.
func cleanTokens(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.ToUpper(strings.TrimSpace(t))
		if len(t) < 2 || seen[t] || !isTokenWord(t) {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func isTokenWord(s string) bool {
	for _, r := range s {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// httpRecorder collects a file's HTTP records and writes them to the builder.
type httpRecorder struct {
	b      *builder
	module graph.NodeID
	// owners are the file's functions and methods, smallest span last wins.
	owners []ownerSpan
}

type ownerSpan struct {
	id         graph.NodeID
	start, end int
}

func newHTTPRecorder(b *builder, module graph.NodeID) *httpRecorder {
	r := &httpRecorder{b: b, module: module}
	for id, n := range b.nodes {
		if n.Kind != KindFunction && n.Kind != KindMethod {
			continue
		}
		s, _ := strconv.Atoi(n.Props["start_line"])
		e, _ := strconv.Atoi(n.Props["end_line"])
		if s > 0 && e >= s {
			r.owners = append(r.owners, ownerSpan{id: id, start: s, end: e})
		}
	}
	sort.Slice(r.owners, func(i, j int) bool { return r.owners[i].id < r.owners[j].id })
	return r
}

// ownerAt is the innermost function or method spanning line, else the module.
func (r *httpRecorder) ownerAt(line int) graph.NodeID {
	best := r.module
	bestSize := -1
	for _, o := range r.owners {
		if line < o.start || line > o.end {
			continue
		}
		if size := o.end - o.start; bestSize < 0 || size < bestSize {
			best, bestSize = o.id, size
		}
	}
	return best
}

// ownerOfSpan is the function or method declared exactly at a function
// node's span, if any (an anonymous callback has none).
func (r *httpRecorder) ownerOfSpan(fn *tree_sitter.Node) graph.NodeID {
	if fn == nil {
		return ""
	}
	start, end := lineOf(fn), int(fn.EndPosition().Row)+1
	for _, o := range r.owners {
		if o.start == start && o.end == end {
			return o.id
		}
	}
	return ""
}

// recordOwner is the declaration a call belongs to: the function whose body
// it is in, else the innermost declaration spanning it, else the module.
func (r *httpRecorder) recordOwner(scopeFn *tree_sitter.Node, line int) graph.NodeID {
	if id := r.ownerOfSpan(scopeFn); id != "" {
		return id
	}
	return r.ownerAt(line)
}

func lineOf(n *tree_sitter.Node) int { return int(n.StartPosition().Row) + 1 }

func (r *httpRecorder) route(method, path, router, handler string, line int) {
	r.b.addListProp(r.module, PropHTTPRoutes, EncodeRecord(method, path, router, handler, strconv.Itoa(line)))
}

func (r *httpRecorder) router(name, prefix string, exported bool, kind string) {
	exp := ""
	if exported {
		exp = "1"
	}
	r.b.addListProp(r.module, PropHTTPRouters, EncodeRecord(name, prefix, exp, kind))
}

func (r *httpRecorder) mount(prefix, router, target string, specs []string) {
	r.b.addListProp(r.module, PropHTTPMounts, EncodeRecord(prefix, router, target, strings.Join(specs, ";")))
}

// envRead records an env var read in the file and, inside a declaration, on
// that declaration too, so a base-URL helper called from another file
// (getBackendUrl()) still names the env var it reads.
func (r *httpRecorder) envRead(name string, scopeFn *tree_sitter.Node, line int) {
	if name == "" {
		return
	}
	name = strings.ToUpper(name)
	r.b.addListProp(r.module, PropEnvReads, name)
	if line > 0 {
		if owner := r.recordOwner(scopeFn, line); owner != r.module {
			r.b.addListProp(owner, PropEnvReads, name)
		}
	}
}

// direct records a call to an HTTP client; a URL taken from the enclosing
// function's parameter makes that function a wrapper instead.
func (r *httpRecorder) direct(method string, url strVal, extraTokens []string, line int, scopeFn *tree_sitter.Node) {
	tokens, tail, param := splitURL(url)
	tokens = cleanTokens(append(tokens, extraTokens...))
	if param >= 0 {
		if owner := r.ownerOfSpan(scopeFn); owner != "" {
			r.b.addListProp(owner, PropHTTPWrapper, EncodeRecord(strconv.Itoa(param), method, JoinTokens(tokens)))
		}
		return
	}
	if tail == "" {
		return
	}
	r.b.addListProp(r.recordOwner(scopeFn, line), PropHTTPCalls,
		EncodeRecord("direct", "", "", method, JoinTokens(tokens), tail, "", strconv.Itoa(line)))
}

// wrapperCall records a call that passes a path (or the caller's own path
// parameter) to a function that may be an HTTP wrapper.
func (r *httpRecorder) wrapperCall(callee string, arg int, method string, url strVal, line int, scopeFn *tree_sitter.Node) {
	tokens, tail, param := splitURL(url)
	p := ""
	if param >= 0 {
		if !requestLikeName(callee) {
			return // passing a parameter on is only a wrapper chain when it looks like a request
		}
		if r.ownerOfSpan(scopeFn) == "" {
			return
		}
		p = strconv.Itoa(param)
	} else if tail == "" {
		return
	}
	r.b.addListProp(r.recordOwner(scopeFn, line), PropHTTPCalls,
		EncodeRecord("wrapper", callee, strconv.Itoa(arg), method, JoinTokens(cleanTokens(tokens)), tail, p, strconv.Itoa(line)))
}

// requestLikeName reports a callee that may send a request: an HTTP verb
// (apiGet, _post) or a name like request, fetch, send, call.
func requestLikeName(callee string) bool {
	if verbHint(callee) != AnyMethod {
		return true
	}
	l := strings.ToLower(callee)
	for _, w := range []string{"request", "fetch", "http", "send", "call", "api"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// notRequestMethod reports string and logging methods that take path-like
// arguments but never send a request (url.includes('/auth'), log.info('/x')).
func notRequestMethod(callee string) bool {
	last := callee
	if i := strings.LastIndex(callee, "."); i >= 0 {
		last = callee[i+1:]
	}
	switch last {
	case "includes", "startsWith", "endsWith", "startswith", "endswith", "indexOf", "replace", "split",
		"match", "test", "push", "join", "log", "warn", "error", "info", "debug", "exception",
		"set", "has", "add", "append", "format", "print", "assert", "assertEqual", "toBe", "toEqual",
		"toHaveBeenCalledWith", "toContain", "navigate", "redirect", "Redirect", "assign", "pushState",
		"replaceState", "send", "json", "render", "sendFile", "open", "exists", "isfile", "isdir":
		return true
	}
	return false
}

func hasLiteralSegment(p string) bool {
	for _, s := range strings.Split(p, "/") {
		if s != "" && s != "{}" {
			return true
		}
	}
	return false
}
