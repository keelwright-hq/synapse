package bind

import (
	"sort"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// HTTPConfig is a member's declared HTTP identity (synapse.yaml http:).
type HTTPConfig struct {
	// BaseEnv are the env var names other members read to reach this one.
	BaseEnv []string
	// GatewayPrefix is stripped from client paths before route matching.
	GatewayPrefix string
}

// Match reasons for HTTP edges.
const (
	MatchRoute       = "route"        // handler implements its route
	MatchDeclaredEnv = "declared_env" // client base URL reads a declared env var
	MatchPathSuffix  = "path_suffix"  // no declared target; unique path match
	MatchEnvRead     = "env_read"     // member reads another member's env var
)

// Service nodes stand for a whole member in env_read edges.
const (
	PropHTTPService = "http_service"
	servicePath     = ".synapse"
)

// ServiceNodeID is the node standing for a declared member.
func ServiceNodeID(member string) graph.NodeID {
	return graph.NodeID("service:" + servicePath + "#" + member)
}

// HTTPStats counts the HTTP binding outcome.
type HTTPStats struct {
	Routes, Clients, Declared, Inferred, Ambiguous, Unmatched, ServiceEdges int
	// EnvClashes are base_env names declared by more than one member; calls
	// reading them are not treated as declared.
	EnvClashes []string
}

type httpRoute struct {
	node    graph.Node
	repo    string
	store   graph.Store
	method  string
	segs    []string
	handler string
	key     string // file and declared route: the same route under several mounts
}

type httpMember struct {
	m      Member
	routes []httpRoute
	// common are route prefixes shared by many of the member's routes
	// (/api, /api/v1): the part of a route a client's base URL supplies.
	common  map[string]bool
	callers []graph.Node
	modules []graph.Node
	aliases map[string][]string
	service graph.Node
}

// bindHTTP links code routes to their handlers (implements) and client calls
// to routes (consumes). A call whose base URL reads an env var declared by
// exactly one member is matched against that member's routes; a call with no
// declared target is linked only when exactly one other member has a unique
// best route for its path. Members that read a declared env var also get an
// env_read edge to that member's service node.
func bindHTTP(opts Options, routes map[string][]graph.Node, write edgeWriter) (HTTPStats, error) {
	var st HTTPStats
	members := make([]*httpMember, 0, len(opts.Members))
	envOwner := map[string]string{}
	envClash := map[string]bool{}
	byName := map[string]*httpMember{}
	for _, m := range opts.Members {
		hm := &httpMember{m: m, aliases: map[string][]string{}}
		members = append(members, hm)
		byName[m.Name] = hm
		if err := deleteServiceNodes(m.Store); err != nil {
			return st, err
		}
		if m.HTTP != nil {
			for _, env := range m.HTTP.BaseEnv {
				env = strings.ToUpper(env)
				if prev, ok := envOwner[env]; ok && prev != m.Name {
					envClash[env] = true
				}
				envOwner[env] = m.Name
			}
			svc, err := putServiceNode(m)
			if err != nil {
				return st, err
			}
			hm.service = svc
		}
		for _, op := range routes[m.Name] {
			if op.Props["unmounted"] == "1" {
				continue // prefix unknown: its path would match by accident
			}
			hm.routes = append(hm.routes, httpRoute{
				node: op, repo: m.Name, store: m.Store,
				method:  op.Props["method"],
				segs:    pathSegments(op.Props["path"]),
				handler: op.Props["handler"],
				key:     op.Path + "#" + op.Props["route"],
			})
		}
		hm.common = commonPrefixes(hm.routes)
		st.Routes += len(hm.routes)
		err := m.Store.ForEachNode(func(n graph.Node) bool {
			if n.Props[parse.PropHTTPClients] != "" {
				hm.callers = append(hm.callers, n)
			}
			if n.Kind == parse.KindModule && (n.Props[parse.PropEnvReads] != "" || n.Props[parse.PropEnvAliases] != "") {
				hm.modules = append(hm.modules, n)
				for _, pair := range strings.Split(n.Props[parse.PropEnvAliases], ",") {
					if f, a, ok := strings.Cut(pair, "="); ok {
						hm.aliases[f] = append(hm.aliases[f], a)
					}
				}
			}
			return true
		})
		if err != nil {
			return st, err
		}
	}
	for env := range envClash {
		delete(envOwner, env)
		st.EnvClashes = append(st.EnvClashes, env)
	}
	sort.Strings(st.EnvClashes)

	// Handlers implement their routes.
	for _, hm := range members {
		for _, r := range hm.routes {
			if r.handler == "" {
				continue
			}
			h, err := hm.m.Store.GetNode(graph.NodeID(r.handler))
			if err != nil {
				continue
			}
			if err := write(hm.m.Name, hm.m.Store, h, hm.m.Name, r.node, parse.EdgeImplements,
				httpEdgeProps(parse.ProvenanceExtracted, MatchRoute, nil)); err != nil {
				return st, err
			}
		}
	}

	for _, hm := range members {
		for _, c := range hm.callers {
			for _, rec := range parse.DecodeRecords(c.Props[parse.PropHTTPClients]) {
				if len(rec) < 4 {
					continue
				}
				st.Clients++
				method, tokens, tail, line := rec[0], expandAliases(parse.SplitTokens(rec[1]), hm.aliases), rec[2], rec[3]
				targets := map[string]string{} // member -> env
				for _, t := range tokens {
					if owner, ok := envOwner[t]; ok {
						targets[owner] = t
					}
				}
				extra := map[string]string{"tail": tail, "line": line}
				var hits []httpRoute
				var match, provenance string
				switch len(targets) {
				case 0:
					hits = inferRoutes(members, hm.m.Name, method, pathSegments(tail))
					match, provenance = MatchPathSuffix, parse.ProvenanceInferred
				case 1:
					for name, env := range targets {
						t := byName[name]
						hits = matchRoutes(t.routes, nil, method, pathSegments(stripGateway(t.m.HTTP, tail)), 1)
						extra["env"] = env
					}
					match, provenance = MatchDeclaredEnv, parse.ProvenanceExtracted
				default:
					st.Ambiguous++
					continue
				}
				if len(hits) == 0 {
					st.Unmatched++
					continue
				}
				if match == MatchDeclaredEnv {
					st.Declared++
				} else {
					st.Inferred++
				}
				for _, r := range hits {
					if err := write(hm.m.Name, hm.m.Store, c, r.repo, r.node, parse.EdgeConsumes,
						httpEdgeProps(provenance, match, extra)); err != nil {
						return st, err
					}
				}
			}
		}
	}

	// Service-level dependencies: a member reading another's base env var.
	for _, hm := range members {
		for _, mod := range hm.modules {
			for _, env := range expandAliases(strings.Split(mod.Props[parse.PropEnvReads], ","), hm.aliases) {
				owner, ok := envOwner[strings.ToUpper(env)]
				if !ok || owner == hm.m.Name {
					continue
				}
				t := byName[owner]
				if err := write(hm.m.Name, hm.m.Store, mod, owner, t.service, parse.EdgeConsumes,
					httpEdgeProps(parse.ProvenanceExtracted, MatchEnvRead, map[string]string{"env": env})); err != nil {
					return st, err
				}
				st.ServiceEdges++
			}
		}
	}
	return st, nil
}

func httpEdgeProps(provenance, match string, extra map[string]string) map[string]string {
	props := map[string]string{parse.PropProvenance: provenance, "match": match}
	for k, v := range extra {
		props[k] = v
	}
	return props
}

func expandAliases(tokens []string, aliases map[string][]string) []string {
	out := append([]string{}, tokens...)
	for _, t := range tokens {
		out = append(out, aliases[t]...)
	}
	return out
}

func stripGateway(cfg *HTTPConfig, tail string) string {
	if cfg == nil || cfg.GatewayPrefix == "" {
		return tail
	}
	p := parse.NormalizeHTTPPath(cfg.GatewayPrefix)
	if tail == p {
		return "/"
	}
	if strings.HasPrefix(tail, p+"/") {
		return tail[len(p):]
	}
	return tail
}

func pathSegments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// inferRoutes links a call with no declared target when exactly one other
// member has a best match, and the path has at least two literal segments.
// A call that also matches its own member's routes (a test calling its own
// API, which a sibling service happens to mirror) is not linked.
func inferRoutes(members []*httpMember, self, method string, tail []string) []httpRoute {
	var found []httpRoute
	owners := 0
	for _, hm := range members {
		hits := matchRoutes(hm.routes, hm.common, method, tail, 2)
		if len(hits) == 0 {
			continue
		}
		if hm.m.Name == self {
			return nil
		}
		found = hits
		owners++
	}
	if owners != 1 {
		return nil
	}
	return found
}

// matchRoutes returns the routes whose path ends with tail (segment by
// segment, {} matching anything) with the most literal segments in common,
// then the fewest segments before the tail (the part a base URL supplies).
// With common set (inference, any member), the part before the tail must
// also be one of the member's common prefixes, so /athletes matches
// /api/v1/athletes but not /api/v1/teams/{}/athletes; with common nil (the
// declared member) any prefix may be the base's. Ties are kept only when
// every tied route is one declared route mounted at several prefixes;
// otherwise the call is ambiguous and nothing is returned.
func matchRoutes(routes []httpRoute, common map[string]bool, method string, tail []string, minLiteral int) []httpRoute {
	if len(tail) == 0 {
		return nil
	}
	bestLit, bestOff := -1, 0
	var hits []httpRoute
	for _, r := range routes {
		if !methodsAgree(method, r.method) || len(tail) > len(r.segs) {
			continue
		}
		off := len(r.segs) - len(tail)
		if common != nil && off > 0 && !common[strings.Join(r.segs[:off], "/")] {
			continue
		}
		lit, specific := 0, 0
		ok := true
		for i, t := range tail {
			s := r.segs[off+i]
			if t == "{}" || s == "{}" {
				continue
			}
			if t != s {
				ok = false
				break
			}
			lit++
			if !genericSegment(t) {
				specific++
			}
		}
		if !ok || specific < minLiteral {
			continue
		}
		switch {
		case lit > bestLit || lit == bestLit && off < bestOff:
			bestLit, bestOff, hits = lit, off, []httpRoute{r}
		case lit == bestLit && off == bestOff:
			hits = append(hits, r)
		}
	}
	if len(hits) > 1 {
		k := hits[0].key
		for _, r := range hits[1:] {
			if r.key != k {
				return nil
			}
		}
	}
	return hits
}

func methodsAgree(a, b string) bool {
	return a == "" || b == "" || a == parse.AnyMethod || b == parse.AnyMethod || strings.EqualFold(a, b)
}

func putServiceNode(m Member) (graph.Node, error) {
	id := ServiceNodeID(m.Name)
	n := graph.Node{
		ID: id, Kind: parse.KindService, Name: m.Name, Path: servicePath,
		Props: map[string]string{PropHTTPService: "1"},
	}
	if canonical, ok, err := uri.Assign(m.Name, servicePath, parse.KindService, m.Name, string(id)); err == nil && ok {
		n.Props[uri.PropKey] = canonical
	}
	return n, m.Store.PutNode(n)
}

func deleteServiceNodes(s graph.Store) error {
	var ids []graph.NodeID
	err := s.ForEachNode(func(n graph.Node) bool {
		if n.Props[PropHTTPService] == "1" {
			ids = append(ids, n.ID)
		}
		return true
	})
	if err != nil {
		return err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := s.DeleteNode(id); err != nil && err != graph.ErrNotFound {
			return err
		}
	}
	return nil
}

// commonPrefixes are the route prefixes (up to three segments) a base URL
// may supply: prefixes of only generic segments (/api, /api/v1), and any
// prefix that at least two routes and a fifth of the member's routes share.
func commonPrefixes(routes []httpRoute) map[string]bool {
	count := map[string]int{}
	out := map[string]bool{}
	for _, r := range routes {
		for k := 1; k <= 3 && k < len(r.segs); k++ {
			p := strings.Join(r.segs[:k], "/")
			count[p]++
			generic := true
			for _, s := range r.segs[:k] {
				generic = generic && genericSegment(s)
			}
			if generic {
				out[p] = true
			}
		}
	}
	min := len(routes) / 5
	if min < 2 {
		min = 2
	}
	for p, c := range count {
		if c >= min {
			out[p] = true
		}
	}
	return out
}

func genericSegment(s string) bool {
	switch s {
	case "api", "health", "healthz", "docs", "status", "openapi.json", "metrics", "ready", "live":
		return true
	}
	return len(s) >= 2 && s[0] == 'v' && strings.Trim(s[1:], "0123456789") == ""
}
