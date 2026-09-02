package parse

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Language is a registered grammar keyed by file extension.
type Language struct {
	Name     string
	Language *tree_sitter.Language
	Extract  Extractor
}

// Extractor turns a parsed tree into graph IR.
type Extractor func(path string, src []byte, root *tree_sitter.Node) Result

// Registry maps file extensions (with leading dot, lower-case) to languages.
type Registry struct {
	mu   sync.RWMutex
	byExt map[string]*Language
}

// NewRegistry returns a registry with Go, TypeScript, and TSX pre-registered.
func NewRegistry() *Registry {
	r := &Registry{byExt: make(map[string]*Language)}
	r.Register(".go", &Language{
		Name:     "go",
		Language: tree_sitter.NewLanguage(tree_sitter_go.Language()),
		Extract:  extractGo,
	})
	r.Register(".ts", &Language{
		Name:     "typescript",
		Language: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()),
		Extract:  extractTypeScript,
	})
	r.Register(".tsx", &Language{
		Name:     "tsx",
		Language: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()),
		Extract:  extractTypeScript,
	})
	return r
}

// Register associates an extension with a language. ext may be ".go" or "go".
func (r *Registry) Register(ext string, lang *Language) {
	if lang == nil {
		return
	}
	ext = normalizeExt(ext)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byExt[ext] = lang
}

// Lookup returns the language for path's extension, or nil if unknown.
func (r *Registry) Lookup(path string) *Language {
	ext := normalizeExt(filepath.Ext(path))
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byExt[ext]
}

func normalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

// newParserFor builds a parser for lang. Caller must Close it.
func newParserFor(lang *Language) (*tree_sitter.Parser, error) {
	if lang == nil || lang.Language == nil {
		return nil, fmt.Errorf("parse: nil language")
	}
	p := tree_sitter.NewParser()
	if err := p.SetLanguage(lang.Language); err != nil {
		p.Close()
		return nil, fmt.Errorf("parse: set language %s: %w", lang.Name, err)
	}
	return p, nil
}
