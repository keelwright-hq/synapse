package badger

import (
	"os"
	"path/filepath"
)

// RepoDir returns the data subdirectory for a workspace member repo.
// Graph data lives at {RepoDir}/graph via Open / OpenWithRepo.
func RepoDir(dataDir, repoName string) string {
	return filepath.Join(dataDir, "repos", repoName)
}

// RepoGraphDir returns the Badger graph directory for a workspace member.
func RepoGraphDir(dataDir, repoName string) string {
	return filepath.Join(RepoDir(dataDir, repoName), "graph")
}

// ShardExists reports whether a member graph directory already exists on disk
// (used to soft-skip missing shards without creating empty Badger DBs).
func ShardExists(dataDir, repoName string) bool {
	info, err := os.Stat(RepoGraphDir(dataDir, repoName))
	return err == nil && info.IsDir()
}

// OverlayExists reports whether the workspace overlay graph directory exists.
func OverlayExists(dataDir string) bool {
	info, err := os.Stat(filepath.Join(OverlayDir(dataDir), "graph"))
	return err == nil && info.IsDir()
}

// SingleRepoExists reports whether a single-repo Badger graph directory exists
// at dataDir/graph (or dataDir itself if already named "graph").
func SingleRepoExists(dataDir string) bool {
	cleaned := filepath.Clean(dataDir)
	graphPath := filepath.Join(cleaned, "graph")
	if filepath.Base(cleaned) == "graph" {
		graphPath = cleaned
	}
	info, err := os.Stat(graphPath)
	return err == nil && info.IsDir()
}

// SingleRepoName returns the repo name recorded in the single-repo store at
// dataDir/graph without migrating or renaming it ("" when none is recorded).
func SingleRepoName(dataDir string) (string, error) {
	s, err := Open(dataDir)
	if err != nil {
		return "", err
	}
	defer s.Close()
	return s.RepoName()
}

// SingleRepoMatches reports whether the single-repo store at dataDir/graph
// belongs to repoName. Stores with no recorded name are treated as a match.
func SingleRepoMatches(dataDir, repoName string) (bool, error) {
	name, err := SingleRepoName(dataDir)
	if err != nil {
		return false, err
	}
	return name == "" || name == repoName, nil
}

// RepoLayout identifies where a repository graph store lives under dataDir.
type RepoLayout string

const (
	// LayoutSingle stores a single-repo graph at dataDir/graph.
	LayoutSingle RepoLayout = "single"
	// LayoutShard stores a workspace member shard at dataDir/repos/<repoName>/graph.
	LayoutShard RepoLayout = "shard"
)

// ResolveRepoLayout inspects dataDir for an existing store for repoName.
// When both a workspace shard (dataDir/repos/<repoName>/graph) and a single-repo
// store (dataDir/graph) exist on disk, the workspace shard wins so export,
// import, query, mcp, and otel agree on which store to open.
func ResolveRepoLayout(dataDir, repoName string) (RepoLayout, bool) {
	if ShardExists(dataDir, repoName) {
		return LayoutShard, true
	}
	if SingleRepoExists(dataDir) {
		return LayoutSingle, true
	}
	return "", false
}

// OpenLayout opens (or creates) the Badger store for repoName using layout.
// An unknown or empty layout defaults to LayoutSingle.
func OpenLayout(dataDir, repoName string, layout RepoLayout) (*Store, error) {
	if layout == LayoutShard {
		return OpenRepo(dataDir, repoName)
	}
	return OpenWithRepo(dataDir, repoName)
}

// OpenSingleOrRepo opens an existing store for repoName (preferring a workspace
// shard at dataDir/repos/<repoName>/graph over dataDir/graph when present), or
// creates a single-repo store at dataDir/graph when neither exists on disk.
func OpenSingleOrRepo(dataDir, repoName string) (*Store, error) {
	if layout, ok := ResolveRepoLayout(dataDir, repoName); ok {
		return OpenLayout(dataDir, repoName, layout)
	}
	return OpenLayout(dataDir, repoName, LayoutSingle)
}

// OpenRepo opens (and migrates) the Badger store for one workspace member
// under dataDir/repos/<repoName>/graph.
func OpenRepo(dataDir, repoName string) (*Store, error) {
	return OpenWithRepo(RepoDir(dataDir, repoName), repoName)
}

// OverlayDir returns the workspace overlay graph directory.
func OverlayDir(dataDir string) string {
	return filepath.Join(dataDir, "overlay")
}

// OpenOverlay opens the workspace overlay Badger store used for cross-repo
// contract edges (implements/consumes). Stub node IDs are repo:// URIs.
func OpenOverlay(dataDir string) (*Store, error) {
	return Open(OverlayDir(dataDir))
}
