package cli

import (
	"fmt"
	"log/slog"

	"github.com/keelwright-hq/synapse/internal/config"
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/store/badger"
	"github.com/keelwright-hq/synapse/internal/store/federated"
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

// openResult is a workspace store plus soft-fail warnings from shard open.
type openResult struct {
	Store     graph.Store
	Workspace *config.Workspace
	Closer    closer
	Warnings  []string
	Fed       *federated.Store // non-nil when federating
}

// openWorkspaceStore loads the workspace and opens either one scoped member DB
// or a per-query federated view of available members. Missing shards are skipped
// with warnings (SYN-16 / SYN-67). scopeRepo is the --repo flag (empty = federate).
func openWorkspaceStore(wsPath, dataDir, scopeRepo string) (openResult, error) {
	ws, err := config.Load(wsPath)
	if err != nil {
		return openResult{}, err
	}
	if scopeRepo != "" {
		member, err := ws.Lookup(scopeRepo)
		if err != nil {
			return openResult{}, err
		}
		if !badger.ShardExists(dataDir, member.Name) {
			return openResult{}, fmt.Errorf("shard %q not found under %s", member.Name, dataDir)
		}
		s, err := badger.OpenRepo(dataDir, member.Name)
		if err != nil {
			return openResult{}, err
		}
		return openResult{Store: s, Workspace: ws, Closer: s}, nil
	}

	var warnings []string
	repos := ws.Repos
	if len(repos) > federated.DefaultMaxShards {
		for _, r := range repos[federated.DefaultMaxShards:] {
			warnings = append(warnings, fmt.Sprintf("max shards (%d) exceeded; skipping member %q",
				federated.DefaultMaxShards, r.Name))
		}
		repos = repos[:federated.DefaultMaxShards]
	}

	members := make([]federated.Member, 0, len(repos))
	stores := make([]*badger.Store, 0, len(repos))
	for _, r := range repos {
		if !badger.ShardExists(dataDir, r.Name) {
			warnings = append(warnings, fmt.Sprintf("missing shard %q (no graph under %s)",
				r.Name, badger.RepoGraphDir(dataDir, r.Name)))
			continue
		}
		s, err := badger.OpenRepo(dataDir, r.Name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("unopenable shard %q: %v", r.Name, err))
			continue
		}
		stores = append(stores, s)
		members = append(members, federated.Member{Name: r.Name, Store: s})
	}
	if len(members) == 0 {
		for _, s := range stores {
			_ = s.Close()
		}
		return openResult{}, fmt.Errorf("no shards available under %s (workspace lists %d repos)", dataDir, len(ws.Repos))
	}

	var overlay graph.Store
	var overlayStore *badger.Store
	if badger.OverlayExists(dataDir) {
		overlayStore, err = badger.OpenOverlay(dataDir)
		if err != nil {
			for _, opened := range stores {
				_ = opened.Close()
			}
			return openResult{}, fmt.Errorf("open overlay: %w", err)
		}
		overlay = overlayStore
	} else {
		warnings = append(warnings, "overlay missing; cross-repo contract edges unavailable")
	}

	fed, err := federated.NewWithOptions(members, federated.Options{
		Overlay:       overlay,
		MaxShards:     federated.DefaultMaxShards,
		LookupTimeout: federated.DefaultLookupTimeout,
	})
	if err != nil {
		if overlayStore != nil {
			_ = overlayStore.Close()
		}
		for _, opened := range stores {
			_ = opened.Close()
		}
		return openResult{}, err
	}
	// Include any max-shard warnings raised inside New (should be empty when pre-truncated).
	warnings = append(warnings, fed.TakeWarnings()...)

	closers := make(multiCloser, 0, len(stores)+2)
	closers = append(closers, fed) // clears pins
	if overlayStore != nil {
		closers = append(closers, overlayStore)
	}
	for _, s := range stores {
		closers = append(closers, s)
	}
	return openResult{
		Store:     fed,
		Workspace: ws,
		Closer:    closers,
		Warnings:  warnings,
		Fed:       fed,
	}, nil
}

// logWarnings writes federation warnings to the given logger (stderr in CLI).
func logWarnings(logger *slog.Logger, warnings []string) {
	if logger == nil {
		return
	}
	for _, w := range warnings {
		logger.Warn(w)
	}
}
