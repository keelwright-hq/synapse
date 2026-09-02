package parse

import (
	"fmt"
	"os"
	"path/filepath"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ParseFile reads path and extracts IR using reg. Unknown extensions yield Skipped=true.
func ParseFile(reg *Registry, path string) (Result, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	return ParseSource(reg, path, src)
}

// ParseSource parses src as path using reg.
func ParseSource(reg *Registry, path string, src []byte) (Result, error) {
	if reg == nil {
		reg = NewRegistry()
	}
	lang := reg.Lookup(path)
	if lang == nil {
		return Result{Path: path, Skipped: true}, nil
	}
	parser, err := newParserFor(lang)
	if err != nil {
		return Result{}, err
	}
	defer parser.Close()

	tree := parser.Parse(src, nil)
	if tree == nil {
		return Result{}, fmt.Errorf("parse: failed to parse %s", path)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return Result{}, fmt.Errorf("parse: nil root for %s", path)
	}

	rel := filepath.ToSlash(path)
	var res Result
	if lang.Extract != nil {
		res = lang.Extract(rel, src, root)
	}
	res.Path = rel
	res.Lang = lang.Name
	res.Normalize()
	return res, nil
}

// ParseTree is a helper for tests that already have a root node.
func ParseTree(path string, src []byte, root *tree_sitter.Node, extract Extractor) Result {
	res := extract(filepath.ToSlash(path), src, root)
	res.Path = filepath.ToSlash(path)
	res.Normalize()
	return res
}
