package tree_sitter_kotlin

// Grammar sources from fwcd/tree-sitter-kotlin@v0.3.2.
// Upstream publishes no Go bindings; we vendor C sources like tree-sitter-swift.

// #cgo CFLAGS: -std=c11 -fPIC -Wno-macro-redefined -I${SRCDIR}/../../src
// #include "../../src/parser.c"
// #include "../../src/scanner.c"
import "C"

import "unsafe"

func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_kotlin())
}
