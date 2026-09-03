package openapi

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Candidate extensions that may contain OpenAPI documents.
var candidateExts = map[string]struct{}{
	".yaml": {},
	".yml":  {},
	".json": {},
}

// ListSpecFiles walks root and returns absolute paths of OpenAPI 3.x docs.
// Directories named in ignoreDirNames are skipped (defaults match parse.DefaultIgnoreDirNames).
func ListSpecFiles(root string, ignoreDirNames []string) ([]string, error) {
	if ignoreDirNames == nil {
		ignoreDirNames = []string{"vendor", "node_modules", ".git", ".synapse"}
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
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable
		}
		if !LooksLikeOpenAPI(data) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}
