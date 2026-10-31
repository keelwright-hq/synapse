package openapi

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/keelwright-hq/synapse/internal/parse"
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
	var files []string
	err := parse.WalkFiles(root, ignoreDirNames, func(path string, d fs.DirEntry) error {
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := candidateExts[ext]; !ok {
			return nil
		}
		// Only sniff a prefix — openapi version is near the top; avoids loading huge YAML/JSON dumps.
		f, err := os.Open(path)
		if err != nil {
			return nil // skip unreadable
		}
		var header [8192]byte
		n, _ := f.Read(header[:])
		_ = f.Close()
		if n == 0 || !LooksLikeOpenAPI(header[:n]) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}
