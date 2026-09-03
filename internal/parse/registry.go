package parse

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	tree_sitter_swift "github.com/keelwright-hq/synapse/third_party/tree-sitter-swift/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
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
	mu    sync.RWMutex
	byExt map[string]*Language
}

// NewRegistry returns a registry with Go, JS/JSX, TypeScript/TSX, Python, and Swift.
func NewRegistry() *Registry {
	r := &Registry{byExt: make(map[string]*Language)}

	goLang := &Language{
		Name:     "go",
		Language: tree_sitter.NewLanguage(tree_sitter_go.Language()),
		Extract:  extractGo,
	}
	jsLang := &Language{
		Name:     "javascript",
		Language: tree_sitter.NewLanguage(tree_sitter_javascript.Language()),
		Extract:  extractJavaScript,
	}
	jsxLang := &Language{
		Name:     "jsx",
		Language: tree_sitter.NewLanguage(tree_sitter_javascript.Language()),
		Extract:  extractJavaScript,
	}
	tsLang := &Language{
		Name:     "typescript",
		Language: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()),
		Extract:  extractTypeScript,
	}
	tsxLang := &Language{
		Name:     "tsx",
		Language: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()),
		Extract:  extractTypeScript,
	}
	pyLang := &Language{
		Name:     "python",
		Language: tree_sitter.NewLanguage(tree_sitter_python.Language()),
		Extract:  extractPython,
	}
	swiftLang := &Language{
		Name:     "swift",
		Language: tree_sitter.NewLanguage(tree_sitter_swift.Language()),
		Extract:  extractSwift,
	}

	r.Register(".go", goLang)
	for _, ext := range []string{".js", ".mjs", ".cjs"} {
		r.Register(ext, jsLang)
	}
	r.Register(".jsx", jsxLang)
	r.Register(".ts", tsLang)
	r.Register(".tsx", tsxLang)
	r.Register(".py", pyLang)
	r.Register(".swift", swiftLang)
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
