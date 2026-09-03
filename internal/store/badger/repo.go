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
