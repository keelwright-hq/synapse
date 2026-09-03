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
