package graphql

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Candidate extensions that may contain GraphQL SDL documents.
var candidateExts = map[string]struct{}{
	".graphql":  {},
	".gql":      {},
	".graphqls": {},
}

// ListSpecFiles walks root and returns absolute paths of GraphQL SDL schema docs.
// Directories named in ignoreDirNames are skipped (defaults match parse.DefaultIgnoreDirNames).
func ListSpecFiles(root string, ignoreDirNames []string) ([]string, error) {
	if ignoreDirNames == nil {
		ignoreDirNames = []string{"vendor", "node_modules", ".git", ".synapse", ".synapse-out"}
	}
	ignoreSet := make(map[string]struct{}, len(ignoreDirNames))
	for _, d := range ignoreDirNames {
		ignoreSet[d] = struct{}{}
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := ignoreSet[d.Name()]; skip && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := candidateExts[ext]; !ok {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil // skip unreadable
		}
		var header [8192]byte
		n, _ := f.Read(header[:])
		_ = f.Close()
		if n == 0 || !LooksLikeGraphQL(header[:n]) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}
