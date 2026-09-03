package cli

import (
	"path/filepath"

	"github.com/keelwright-hq/synapse/internal/uri"
)

// repoName is the optional --repo override (empty → basename of the command root).
var repoName string

// resolveRepoName returns a validated repo:// name from --repo or the basename of root.
func resolveRepoName(root string) (string, error) {
	name := repoName
	if name == "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		name = filepath.Base(abs)
	}
	return uri.NormalizeRepo(name)
}
