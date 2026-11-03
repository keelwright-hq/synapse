package rank

import (
	"errors"
	"fmt"
	"sort"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// repoIterStore is a federated workspace store that can list one member.
type repoIterStore interface {
	repoScopedStore
	ForEachNodeInRepo(repo string, fn func(graph.Node) bool) error
	MemberNames() []string
}

// ServiceSite is one cross-service HTTP dependency: a call site (or, for
// match env_read, a file reading the target's base URL variable) and the
// route it reaches.
type ServiceSite struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Line      int    `json:"line,omitempty"`
	Symbol    string `json:"symbol"`
	Target    string `json:"target"`
	Route     string `json:"route,omitempty"`      // METHOD /path
	RouteFile string `json:"route_file,omitempty"` // file declaring the route
	Match     string `json:"match"`
	Env       string `json:"env,omitempty"`
}

// ServiceLink groups the sites between a member and one other member.
type ServiceLink struct {
	Repo     string        `json:"repo"`
	Depth    int           `json:"depth"`
	Via      string        `json:"via,omitempty"` // for depth > 1: the member it reaches the target through
	Calls    int           `json:"calls"`         // route-level call sites
	EnvReads []string      `json:"env_reads,omitempty"`
	Sites    []ServiceSite `json:"sites"`
}

// ServiceResult answers which members call a member, or which it calls.
type ServiceResult struct {
	Member    string        `json:"member"`
	Direction string        `json:"direction"` // consumers | calls
	Services  []ServiceLink `json:"services"`
}

func asRepoIter(store graph.Store) (repoIterStore, error) {
	ri, ok := store.(repoIterStore)
	if !ok {
		return nil, fmt.Errorf("rank: service queries need a workspace (--workspace)")
	}
	return ri, nil
}

// isHTTPTarget reports a code route or a member's service node: the ends of
// HTTP consumes edges.
func isHTTPTarget(n graph.Node) bool {
	return n.Props[propHTTPService] == "1" || n.Kind == parse.KindOperation && n.Props["source"] == "code"
}

// ServiceCalls lists the members that member calls over HTTP, with each call
// site and the route it was linked to.
func ServiceCalls(store graph.Store, member string) (ServiceResult, error) {
	ri, err := asRepoIter(store)
	if err != nil {
		return ServiceResult{}, err
	}
	res := ServiceResult{Member: member, Direction: "calls"}
	var callers []graph.Node
	err = ri.ForEachNodeInRepo(member, func(n graph.Node) bool {
		if n.Props[parse.PropHTTPClients] != "" || n.Kind == parse.KindModule && n.Props[parse.PropEnvReads] != "" {
			callers = append(callers, n)
		}
		return true
	})
	if err != nil {
		return res, err
	}
	links := map[string]*ServiceLink{}
	for _, c := range callers {
		edges, err := ri.OutEdgesInRepo(member, c.ID, parse.EdgeConsumes)
		if err != nil {
			return res, err
		}
		for _, e := range edges {
			to := otherRepo(e, "out", member)
			if to == member {
				continue
			}
			target, err := ri.GetNodeInRepo(to, e.To)
			if err != nil || !isHTTPTarget(target) {
				continue
			}
			addSite(links, to, 1, "", site(member, c, to, target, e))
		}
	}
	res.Services = sortedLinks(links)
	return res, nil
}

// ServiceConsumers lists the members that call member over HTTP. With depth
// > 1 it also lists members that reach it through those callers.
func ServiceConsumers(store graph.Store, member string, depth int) (ServiceResult, error) {
	ri, err := asRepoIter(store)
	if err != nil {
		return ServiceResult{}, err
	}
	if depth <= 0 {
		depth = 1
	}
	res := ServiceResult{Member: member, Direction: "consumers"}
	links := map[string]*ServiceLink{}
	seen := map[string]bool{member: true}
	frontier := []string{member}
	for d := 1; d <= depth && len(frontier) > 0; d++ {
		var next []string
		for _, target := range frontier {
			var ends []graph.Node
			err := ri.ForEachNodeInRepo(target, func(n graph.Node) bool {
				if isHTTPTarget(n) {
					ends = append(ends, n)
				}
				return true
			})
			if err != nil {
				if errors.Is(err, graph.ErrNotFound) {
					continue
				}
				return res, err
			}
			for _, end := range ends {
				edges, err := ri.InEdgesInRepo(target, end.ID, parse.EdgeConsumes)
				if err != nil {
					return res, err
				}
				for _, e := range edges {
					from := otherRepo(e, "in", target)
					if from == target || from == member {
						continue
					}
					if d > 1 && links[from] != nil && links[from].Depth < d {
						continue // already a closer caller
					}
					caller, err := ri.GetNodeInRepo(from, e.From)
					if err != nil {
						continue
					}
					via := ""
					if d > 1 {
						via = target
					}
					addSite(links, from, d, via, site(from, caller, target, end, e))
					if !seen[from] {
						seen[from] = true
						next = append(next, from)
					}
				}
			}
		}
		sort.Strings(next)
		frontier = next
	}
	res.Services = sortedLinks(links)
	return res, nil
}

func site(repo string, caller graph.Node, target string, end graph.Node, e graph.Edge) ServiceSite {
	s := ServiceSite{
		Repo: repo, Path: caller.Path, Symbol: symbolLabel(caller), Target: target,
		Match: e.Props["match"], Env: e.Props["env"],
	}
	s.Line = propInt(e.Props, "line")
	if s.Line == 0 {
		s.Line = propInt(caller.Props, "start_line")
	}
	if end.Kind == parse.KindOperation {
		s.Route = end.Name
		s.RouteFile = end.Path
	}
	return s
}

func addSite(links map[string]*ServiceLink, repo string, depth int, via string, s ServiceSite) {
	l := links[repo]
	if l == nil {
		l = &ServiceLink{Repo: repo, Depth: depth, Via: via}
		links[repo] = l
	}
	switch {
	case s.Route != "":
		l.Calls++
	case s.Env != "" && !containsString(l.EnvReads, s.Env):
		l.EnvReads = append(l.EnvReads, s.Env)
		sort.Strings(l.EnvReads)
	}
	l.Sites = append(l.Sites, s)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sortedLinks(links map[string]*ServiceLink) []ServiceLink {
	out := make([]ServiceLink, 0, len(links))
	for _, l := range links {
		sort.Slice(l.Sites, func(i, j int) bool {
			a, b := l.Sites[i], l.Sites[j]
			if (a.Route == "") != (b.Route == "") {
				return a.Route != "" // route-level sites first
			}
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			return a.Route < b.Route
		})
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].Repo < out[j].Repo
	})
	return out
}

// repoOnly limits seed lookups to one member of a federated store.
type repoOnly struct {
	graph.Store
	ri   repoIterStore
	repo string
}

func (r repoOnly) GetNode(id graph.NodeID) (graph.Node, error) { return r.ri.GetNodeInRepo(r.repo, id) }

func (r repoOnly) ForEachNode(fn func(graph.Node) bool) error {
	return r.ri.ForEachNodeInRepo(r.repo, fn)
}

// ResolveSeedInRepo resolves a seed like ResolveSeed, looking only at one
// workspace member, and pins it there so later reads of the seed stay in
// that member. On a single-repo store it is ResolveSeed.
func ResolveSeedInRepo(store graph.Store, repo, query string) (graph.NodeID, error) {
	ri, ok := store.(repoIterStore)
	if !ok || repo == "" {
		return ResolveSeed(store, query)
	}
	id, err := ResolveSeed(repoOnly{Store: store, ri: ri, repo: repo}, query)
	if err != nil {
		return "", err
	}
	if _, err := ri.GetNodeInRepo(repo, id); err != nil {
		return "", err
	}
	return id, nil
}
