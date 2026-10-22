// Package federated provides a read-only graph.Store over multiple member stores.
package federated

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/uri"
)

// Defaults for OSS federation guardrails (SYN-16 / SYN-69).
const (
	DefaultMaxShards      = 32
	DefaultLookupTimeout  = 5 * time.Second
)

// Member is one named backend store (typically one Badger DB per repo).
type Member struct {
	Name  string
	Store graph.Store
}

// Options configure federated open and lookup guardrails.
type Options struct {
	// MaxShards caps how many members may be attached (0 = DefaultMaxShards).
	MaxShards int
	// LookupTimeout bounds URI / ownership fan-out (0 = DefaultLookupTimeout).
	LookupTimeout time.Duration
	// Overlay holds cross-repo contract edges keyed by repo:// URI.
	Overlay graph.Store
}

// Store is a lightweight, per-query read federation over member stores.
//
// Phase-1 Node.IDs may collide across repos; routing prefers URI pins and
// unique ownership so neighborhood walks stay within the seed's repo.
// Those pins live in idOwner and are mutable query state, so a Store must
// not be shared across goroutines. Keep underlying member stores long-lived
// (to avoid re-opening Badger) and call New or Session once per query.
//
// An optional Overlay holds cross-repo contract edges whose endpoints are
// keyed by repo:// URI (see package bind). Overlay edges are merged into
// OutEdges/InEdges and far ends resolve via GetNodeByURI.
//
// Close does not close members or the overlay; the caller owns their lifetime.
type Store struct {
	members       []Member
	overlay       graph.Store
	lookupTimeout time.Duration

	mu       sync.RWMutex
	idOwner  map[graph.NodeID]int // pinned or uniquely owned member index
	warnings []string
}

// New builds a federated store for a single query. members must be non-empty
// and have unique names. The returned Store is not safe for concurrent use.
func New(members []Member) (*Store, error) {
	return NewWithOptions(members, Options{})
}

// NewWithOverlay is like New but also merges cross-repo edges from overlay.
func NewWithOverlay(members []Member, overlay graph.Store) (*Store, error) {
	return NewWithOptions(members, Options{Overlay: overlay})
}

// NewWithOptions builds a federated store with guardrails.
func NewWithOptions(members []Member, opts Options) (*Store, error) {
	if len(members) == 0 {
		return nil, fmt.Errorf("federated: no members")
	}
	maxShards := opts.MaxShards
	if maxShards <= 0 {
		maxShards = DefaultMaxShards
	}
	timeout := opts.LookupTimeout
	if timeout <= 0 {
		timeout = DefaultLookupTimeout
	}

	// Copy so callers can reuse their slice without racing our Name normalization.
	copied := make([]Member, len(members))
	copy(copied, members)
	seen := make(map[string]struct{}, len(copied))
	for i, m := range copied {
		if m.Store == nil {
			return nil, fmt.Errorf("federated: member %d has nil store", i)
		}
		name, err := uri.NormalizeRepo(m.Name)
		if err != nil {
			return nil, fmt.Errorf("federated: member %d: %w", i, err)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("federated: duplicate member name %q", name)
		}
		seen[name] = struct{}{}
		copied[i].Name = name
	}

	s := &Store{
		members:       copied,
		overlay:       opts.Overlay,
		lookupTimeout: timeout,
		idOwner:       make(map[graph.NodeID]int),
	}
	if len(copied) > maxShards {
		skipped := copied[maxShards:]
		s.members = copied[:maxShards]
		for _, m := range skipped {
			s.warn(fmt.Sprintf("max shards (%d) exceeded; skipping member %q", maxShards, m.Name))
		}
	}
	return s, nil
}

// Session returns a new Store sharing the same members (and overlay) with an empty pin map.
// Warnings are not copied; use one Session (or New) per concurrent query against long-lived members.
func (s *Store) Session() *Store {
	return &Store{
		members:       s.members,
		overlay:       s.overlay,
		lookupTimeout: s.lookupTimeout,
		idOwner:       make(map[graph.NodeID]int),
	}
}

// Close clears query pins and warnings. It does not close member stores or the overlay.
func (s *Store) Close() error {
	s.mu.Lock()
	s.idOwner = make(map[graph.NodeID]int)
	s.warnings = nil
	s.mu.Unlock()
	return nil
}

func (s *Store) warn(msg string) {
	s.mu.Lock()
	s.warnings = append(s.warnings, msg)
	s.mu.Unlock()
}

// TakeWarnings returns and clears accumulated warnings (missing far-ends, max shards, …).
func (s *Store) TakeWarnings() []string {
	s.mu.Lock()
	out := append([]string(nil), s.warnings...)
	s.warnings = nil
	s.mu.Unlock()
	return out
}

// Warnings returns a copy of accumulated warnings without clearing them.
func (s *Store) Warnings() []string {
	s.mu.RLock()
	out := append([]string(nil), s.warnings...)
	s.mu.RUnlock()
	return out
}

// AppendWarnings records open-time or caller-supplied warnings.
func (s *Store) AppendWarnings(msgs ...string) {
	if len(msgs) == 0 {
		return
	}
	s.mu.Lock()
	s.warnings = append(s.warnings, msgs...)
	s.mu.Unlock()
}

func (s *Store) lookupCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.lookupTimeout)
}

func (s *Store) pin(id graph.NodeID, idx int) {
	s.mu.Lock()
	s.idOwner[id] = idx
	s.mu.Unlock()
}

func (s *Store) pinIfUnset(id graph.NodeID, idx int) {
	s.mu.Lock()
	if _, ok := s.idOwner[id]; !ok {
		s.idOwner[id] = idx
	}
	s.mu.Unlock()
}

// PinnedRepo returns the logical repo name currently pinned for id, if any.
func (s *Store) PinnedRepo(id graph.NodeID) string {
	if idx, ok := s.owner(id); ok && idx >= 0 && idx < len(s.members) {
		return s.members[idx].Name
	}
	return ""
}

func (s *Store) memberIndexByName(repo string) (int, bool) {
	if repo == "" {
		return -1, false
	}
	for i, m := range s.members {
		if m.Name == repo {
			return i, true
		}
	}
	return -1, false
}

func (s *Store) owner(id graph.NodeID) (int, bool) {
	s.mu.RLock()
	idx, ok := s.idOwner[id]
	s.mu.RUnlock()
	return idx, ok
}

func (s *Store) resolveOwner(id graph.NodeID) (int, error) {
	if idx, ok := s.owner(id); ok {
		return idx, nil
	}
	ctx, cancel := s.lookupCtx()
	defer cancel()

	var found []int
	for i, m := range s.members {
		if err := ctx.Err(); err != nil {
			return -1, fmt.Errorf("federated: lookup timeout: %w", err)
		}
		if _, err := m.Store.GetNode(id); err == nil {
			found = append(found, i)
		} else if err != graph.ErrNotFound {
			return -1, err
		}
	}
	switch len(found) {
	case 0:
		return -1, graph.ErrNotFound
	case 1:
		s.pin(id, found[0])
		return found[0], nil
	default:
		return -1, fmt.Errorf("%w: node id %q exists in %d repos; use a repo:// URI or --repo scope",
			graph.ErrConflict, id, len(found))
	}
}

func (s *Store) PutNode(graph.Node) error {
	return fmt.Errorf("federated: PutNode is not supported (open a single-repo store to index)")
}

func (s *Store) DeleteNode(graph.NodeID) error {
	return fmt.Errorf("federated: DeleteNode is not supported")
}

func (s *Store) PutEdge(graph.Edge) error {
	return fmt.Errorf("federated: PutEdge is not supported")
}

func (s *Store) DeleteEdge(graph.NodeID, graph.NodeID, graph.EdgeType) error {
	return fmt.Errorf("federated: DeleteEdge is not supported")
}

func (s *Store) GetNode(id graph.NodeID) (graph.Node, error) {
	idx, err := s.resolveOwner(id)
	if err != nil {
		return graph.Node{}, err
	}
	return s.members[idx].Store.GetNode(id)
}

// GetNodeInRepo fetches id from the named member shard without overwriting
// an existing cross-repo pin for id.
func (s *Store) GetNodeInRepo(repo string, id graph.NodeID) (graph.Node, error) {
	if idx, ok := s.memberIndexByName(repo); ok {
		n, err := s.members[idx].Store.GetNode(id)
		if err == nil {
			s.pinIfUnset(id, idx)
		}
		return n, err
	}
	return s.GetNode(id)
}

func (s *Store) GetNodeByURI(repoURI string) (graph.Node, error) {
	n, _, err := s.getNodeByURI(repoURI, true)
	return n, err
}

func (s *Store) getNodeByURI(repoURI string, clobberPin bool) (graph.Node, string, error) {
	u, err := uri.Parse(repoURI)
	if err != nil {
		return graph.Node{}, "", err
	}
	canonical := u.String()
	ctx, cancel := s.lookupCtx()
	defer cancel()

	for i, m := range s.members {
		if err := ctx.Err(); err != nil {
			return graph.Node{}, "", fmt.Errorf("federated: lookup timeout: %w", err)
		}
		if m.Name != u.Repo {
			continue
		}
		n, err := m.Store.GetNodeByURI(canonical)
		if err != nil {
			return graph.Node{}, "", err
		}
		if clobberPin {
			s.pin(n.ID, i)
		} else {
			s.pinIfUnset(n.ID, i)
		}
		return n, m.Name, nil
	}
	// Fallback: scan all (covers misnamed members).
	for i, m := range s.members {
		if err := ctx.Err(); err != nil {
			return graph.Node{}, "", fmt.Errorf("federated: lookup timeout: %w", err)
		}
		n, err := m.Store.GetNodeByURI(canonical)
		if err == nil {
			if clobberPin {
				s.pin(n.ID, i)
			} else {
				s.pinIfUnset(n.ID, i)
			}
			return n, m.Name, nil
		}
		if err != graph.ErrNotFound {
			return graph.Node{}, "", err
		}
	}
	return graph.Node{}, "", graph.ErrNotFound
}

func (s *Store) OutEdges(from graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	return s.OutEdgesInRepo("", from, edgeType)
}

// OutEdgesInRepo returns outgoing member + overlay edges for from in repo
// (or resolves repo via owner pin when repo is empty).
func (s *Store) OutEdgesInRepo(repo string, from graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	idx, ok := s.memberIndexByName(repo)
	if !ok {
		var err error
		idx, err = s.resolveOwner(from)
		if err != nil {
			return nil, err
		}
	}
	memberRepo := s.members[idx].Name
	edges, err := s.members[idx].Store.OutEdges(from, edgeType)
	if err != nil {
		return nil, err
	}
	out := make([]graph.Edge, 0, len(edges))
	for _, e := range edges {
		s.pinIfUnset(e.From, idx)
		s.pinIfUnset(e.To, idx)
		out = append(out, stampEdgeRepos(e, memberRepo, memberRepo))
	}
	overlayEdges, err := s.overlayOutForMember(idx, from, edgeType)
	if err != nil {
		return nil, err
	}
	return append(out, overlayEdges...), nil
}

func (s *Store) InEdges(to graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	return s.InEdgesInRepo("", to, edgeType)
}

// InEdgesInRepo returns incoming member + overlay edges for to in repo
// (or resolves repo via owner pin when repo is empty).
func (s *Store) InEdgesInRepo(repo string, to graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	idx, ok := s.memberIndexByName(repo)
	if !ok {
		var err error
		idx, err = s.resolveOwner(to)
		if err != nil {
			return nil, err
		}
	}
	memberRepo := s.members[idx].Name
	edges, err := s.members[idx].Store.InEdges(to, edgeType)
	if err != nil {
		return nil, err
	}
	out := make([]graph.Edge, 0, len(edges))
	for _, e := range edges {
		s.pinIfUnset(e.From, idx)
		s.pinIfUnset(e.To, idx)
		out = append(out, stampEdgeRepos(e, memberRepo, memberRepo))
	}
	overlayEdges, err := s.overlayInForMember(idx, to, edgeType)
	if err != nil {
		return nil, err
	}
	return append(out, overlayEdges...), nil
}

func stampEdgeRepos(e graph.Edge, fromRepo, toRepo string) graph.Edge {
	props := make(map[string]string, len(e.Props)+2)
	for k, v := range e.Props {
		props[k] = v
	}
	if fromRepo != "" {
		props["from_repo"] = fromRepo
	}
	if toRepo != "" {
		props["to_repo"] = toRepo
	}
	e.Props = props
	return e
}

func (s *Store) overlayOutForMember(idx int, from graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	if s.overlay == nil {
		return nil, nil
	}
	n, err := s.members[idx].Store.GetNode(from)
	if err != nil {
		return nil, err
	}
	repoURI := ""
	if n.Props != nil {
		repoURI = n.Props[uri.PropKey]
	}
	if repoURI == "" {
		return nil, nil
	}
	raw, err := s.overlay.OutEdges(graph.NodeID(repoURI), edgeType)
	if err != nil {
		if err == graph.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	fromRepo := s.members[idx].Name
	out := make([]graph.Edge, 0, len(raw))
	for _, e := range raw {
		toNode, toRepo, err := s.getNodeByURI(string(e.To), false)
		if err != nil {
			s.warn(fmt.Sprintf("overlay edge %s → %s unresolved: %v", e.From, e.To, err))
			continue
		}
		out = append(out, stampEdgeRepos(graph.Edge{
			From:  from,
			To:    toNode.ID,
			Type:  e.Type,
			Props: e.Props,
		}, fromRepo, toRepo))
	}
	return out, nil
}

func (s *Store) overlayInForMember(idx int, to graph.NodeID, edgeType graph.EdgeType) ([]graph.Edge, error) {
	if s.overlay == nil {
		return nil, nil
	}
	n, err := s.members[idx].Store.GetNode(to)
	if err != nil {
		return nil, err
	}
	repoURI := ""
	if n.Props != nil {
		repoURI = n.Props[uri.PropKey]
	}
	if repoURI == "" {
		return nil, nil
	}
	raw, err := s.overlay.InEdges(graph.NodeID(repoURI), edgeType)
	if err != nil {
		if err == graph.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	toRepo := s.members[idx].Name
	out := make([]graph.Edge, 0, len(raw))
	for _, e := range raw {
		fromNode, fromRepo, err := s.getNodeByURI(string(e.From), false)
		if err != nil {
			s.warn(fmt.Sprintf("overlay edge %s → %s unresolved: %v", e.From, e.To, err))
			continue
		}
		out = append(out, stampEdgeRepos(graph.Edge{
			From:  fromNode.ID,
			To:    to,
			Type:  e.Type,
			Props: e.Props,
		}, fromRepo, toRepo))
	}
	return out, nil
}

func (s *Store) ForEachNode(fn func(graph.Node) bool) error {
	for _, m := range s.members {
		cont := true
		err := m.Store.ForEachNode(func(n graph.Node) bool {
			cont = fn(n)
			return cont
		})
		if err != nil {
			return err
		}
		if !cont {
			return nil
		}
	}
	return nil
}

// MemberNames returns logical repo names in member order.
func (s *Store) MemberNames() []string {
	out := make([]string, len(s.members))
	for i, m := range s.members {
		out[i] = m.Name
	}
	return out
}
