# tree-sitter-swift (vendored grammar)

Synapse registers Swift via this tree, not `github.com/alex-pinkus/tree-sitter-swift/bindings/go` directly.

Upstream **does not check in** `src/parser.c` (see [their FAQ](https://github.com/alex-pinkus/tree-sitter-swift#where-is-your-parserc)). The published Go module therefore fails to compile (`parser.c: file not found`). We do not write a scanner; `scanner.c` and generated `parser.c` come from the npm package **tree-sitter-swift@0.7.1**.

Refresh:

```bash
npm pack tree-sitter-swift@0.7.1
tar -xzf tree-sitter-swift-0.7.1.tgz
cp package/src/parser.c package/src/scanner.c third_party/tree-sitter-swift/src/
cp package/src/tree_sitter/*.h third_party/tree-sitter-swift/src/tree_sitter/
```
