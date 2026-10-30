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

// OpenSingleOrRepo opens a workspace member shard at dataDir/repos/<repoName>/graph
// when that shard exists on disk and no single-repo store exists at dataDir/graph;
// otherwise it opens the single-repo store at dataDir/graph.
func OpenSingleOrRepo(dataDir, repoName string) (*Store, error) {
	if !SingleRepoExists(dataDir) && ShardExists(dataDir, repoName) {
		return OpenRepo(dataDir, repoName)
	}
	return OpenWithRepo(dataDir, repoName)
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
