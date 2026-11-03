package bind

import (
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
)

func testRoutes(specs ...string) []httpRoute {
	var out []httpRoute
	for _, s := range specs {
		// "METHOD /path key"
		f := strings.Fields(s)
		out = append(out, httpRoute{
			node:   graph.Node{ID: graph.NodeID("operation:" + f[0] + " " + f[1]), Name: f[0] + " " + f[1]},
			method: f[0], segs: pathSegments(f[1]), key: f[2],
		})
	}
	return out
}

func names(rs []httpRoute) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.node.Name)
	}
	return strings.Join(out, ";")
}

func TestMatchRoutes(t *testing.T) {
	routes := testRoutes(
		"GET /api/v1/users users#GET/",
		"GET /ui/users users#GET/", // the same router mounted twice
		"GET /api/v1/teams/{}/users teams#GET/{}/users",
		"POST /api/v1/users users#POST/",
		"GET /api/v1/orders/{} orders#GET/{}",
		"GET /api/v1/admin/orders/{} admin#GET/orders/{}",
		"GET /api/v1/health health#GET/health",
		"GET /api/v1/reports reports#GET/",
		"GET /api/v1/reports/{} reports#GET/{}",
		"GET /api/v1/audit audit#GET/",
		"GET /api/v1/notes notes#GET/",
	)
	common := commonPrefixes(routes)
	for _, c := range []struct {
		name, method, tail string
		min                int
		want               string
	}{
		{"exact", "GET", "/api/v1/users", 1, "GET /api/v1/users"},
		// A base URL ending in /api/v1 supplies the common prefix; /ui is too
		// rare in this member to be one.
		{"suffix after a common prefix", "GET", "/users", 1, "GET /api/v1/users"},
		{"method must agree", "DELETE", "/api/v1/users", 1, ""},
		{"any method", "*", "/api/v1/reports", 1, "GET /api/v1/reports"},
		{"parameters match anything", "GET", "/api/v1/orders/42", 1, "GET /api/v1/orders/{}"},
		// orders/{} also ends admin/orders/{}, but /api/v1/admin is not a
		// common prefix, so only the direct route matches.
		{"leading segments must be common", "GET", "/orders/{}", 1, "GET /api/v1/orders/{}"},
		{"generic segments do not count", "GET", "/api/v1/health", 2, ""},
		{"too few literals for inference", "GET", "/users", 2, ""},
		{"no literal at all", "GET", "/{}", 1, ""},
	} {
		got := names(matchRoutes(routes, common, c.method, pathSegments(c.tail), c.min))
		if got != c.want {
			t.Errorf("%s: matchRoutes(%s %s) = %q, want %q", c.name, c.method, c.tail, got, c.want)
		}
	}

	// The filter does not depend on how many routes a member has: with four,
	// /api/v1/admin is still not a prefix a base URL supplies.
	small := testRoutes("GET /api/v1/orders/{} orders#1", "GET /api/v1/admin/orders/{} admin#1", "GET /api/v1/a a#1", "GET /api/v1/b b#1")
	if got := names(matchRoutes(small, commonPrefixes(small), "GET", pathSegments("/orders/{}"), 1)); got != "GET /api/v1/orders/{}" {
		t.Errorf("small member /orders/{} = %q", got)
	}

	// The declared member (no common filter) takes the unique best route,
	// however deep its prefix, and prefers the shortest prefix on a tie in
	// literals.
	deep := testRoutes("GET /api/v1/internal/svc/users u#1", "GET /api/v1/teams/{}/svc/users t#1")
	if got := names(matchRoutes(deep, nil, "GET", pathSegments("/users"), 1)); got != "GET /api/v1/internal/svc/users" {
		t.Errorf("declared deep prefix = %q", got)
	}
	if got := names(matchRoutes(routes, nil, "GET", pathSegments("/users"), 1)); got != "GET /ui/users" {
		t.Errorf("declared shortest prefix = %q", got)
	}

	// One router mounted under two common prefixes: both mounts are linked.
	mounted := testRoutes("GET /v1/items/{} items#GET/{}", "GET /ui/items/{} items#GET/{}", "GET /v1/x x#1", "GET /ui/x x#1")
	if got := names(matchRoutes(mounted, commonPrefixes(mounted), "GET", pathSegments("/items/{}"), 1)); got != "GET /v1/items/{};GET /ui/items/{}" {
		t.Errorf("same route under two mounts = %q", got)
	}

	// Two different routes tie: ambiguous, nothing linked.
	tied := testRoutes("GET /a/items/{} a#1", "GET /b/items/{} b#1", "GET /a/x a#2", "GET /b/y b#2")
	if got := matchRoutes(tied, commonPrefixes(tied), "GET", pathSegments("/items/{}"), 1); len(got) != 0 {
		t.Errorf("ambiguous tie linked %q", names(got))
	}
}

func TestStripGateway(t *testing.T) {
	cfg := &HTTPConfig{GatewayPrefix: "/svc"}
	for in, want := range map[string]string{
		"/svc/api/v1/x": "/api/v1/x",
		"/svc":          "/",
		"/svcx/a":       "/svcx/a",
		"/api/v1/x":     "/api/v1/x",
	} {
		if got := stripGateway(cfg, in); got != want {
			t.Errorf("stripGateway(%q) = %q, want %q", in, got, want)
		}
	}
	if got := stripGateway(nil, "/svc/a"); got != "/svc/a" {
		t.Errorf("nil config stripped: %q", got)
	}
}

func TestInferRoutesNeedsOneOtherMember(t *testing.T) {
	mk := func(name string, specs ...string) *httpMember {
		hm := &httpMember{m: Member{Name: name}, routes: testRoutes(specs...)}
		for i := range hm.routes {
			hm.routes[i].repo = name
		}
		hm.common = commonPrefixes(hm.routes)
		return hm
	}
	a := mk("a", "GET /api/v1/scores/player/{} s#1")
	b := mk("b", "GET /api/v1/scores/player/{} t#1")
	self := mk("self", "GET /api/v1/other/thing o#1")
	tail := pathSegments("/scores/player/{}")
	if got := inferRoutes([]*httpMember{a, self}, "self", "GET", tail); names(got) != "GET /api/v1/scores/player/{}" {
		t.Errorf("one other member: %q", names(got))
	}
	if got := inferRoutes([]*httpMember{a, b, self}, "self", "GET", tail); len(got) != 0 {
		t.Errorf("two members match: linked %q", names(got))
	}
	// The caller's own member also serves the path (a test calling its own
	// API): not a cross-service call.
	if got := inferRoutes([]*httpMember{a, mk("self", "GET /scores/player/{} me#1")}, "self", "GET", tail); len(got) != 0 {
		t.Errorf("self match: linked %q", names(got))
	}
}
