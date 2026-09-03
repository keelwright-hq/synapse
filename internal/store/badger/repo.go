package badger

import "path/filepath"

// RepoDir returns the data subdirectory for a workspace member repo.
// Graph data lives at {RepoDir}/graph via Open / OpenWithRepo.
func RepoDir(dataDir, repoName string) string {
	return filepath.Join(dataDir, "repos", repoName)
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
