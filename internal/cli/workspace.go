package cli

import (
	"fmt"

	"github.com/taricsa/synapse/internal/config"
	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/store/badger"
	"github.com/taricsa/synapse/internal/store/federated"
)

// workspacePath is the optional --workspace path to synapse.yaml (or its directory).
var workspacePath string

type closer interface {
	Close() error
}

// multiCloser closes several resources; first error wins.
type multiCloser []closer

func (m multiCloser) Close() error {
	var first error
	for _, c := range m {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// openWorkspaceStore loads the workspace and opens either one scoped member DB
// or a per-query federated view of all members. scopeRepo is the --repo flag
// (empty = federate). The returned closer owns Badger member lifetime;
// federated.Store does not close members.
func openWorkspaceStore(wsPath, dataDir, scopeRepo string) (graph.Store, *config.Workspace, closer, error) {
	ws, err := config.Load(wsPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if scopeRepo != "" {
		member, err := ws.Lookup(scopeRepo)
		if err != nil {
			return nil, nil, nil, err
		}
		s, err := badger.OpenRepo(dataDir, member.Name)
		if err != nil {
			return nil, nil, nil, err
		}
		return s, ws, s, nil
	}

	members := make([]federated.Member, 0, len(ws.Repos))
	stores := make([]*badger.Store, 0, len(ws.Repos))
	for _, r := range ws.Repos {
		s, err := badger.OpenRepo(dataDir, r.Name)
		if err != nil {
			for _, opened := range stores {
				_ = opened.Close()
			}
			return nil, nil, nil, fmt.Errorf("open repo %q: %w", r.Name, err)
		}
		stores = append(stores, s)
		members = append(members, federated.Member{Name: r.Name, Store: s})
	}
	fed, err := federated.New(members)
	if err != nil {
		for _, opened := range stores {
			_ = opened.Close()
		}
		return nil, nil, nil, err
	}
	closers := make(multiCloser, 0, len(stores)+1)
	closers = append(closers, fed) // clears pins
	for _, s := range stores {
		closers = append(closers, s)
	}
	return fed, ws, closers, nil
}
