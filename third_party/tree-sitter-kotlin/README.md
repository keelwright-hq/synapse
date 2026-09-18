# tree-sitter-kotlin (vendored grammar)

Synapse registers Kotlin via this tree. Upstream [`fwcd/tree-sitter-kotlin`](https://github.com/fwcd/tree-sitter-kotlin) publishes C sources but no Go bindings.

Refresh from module cache (or a checkout) of **v0.3.2**:

```bash
KT=$(go env GOMODCACHE)/github.com/fwcd/tree-sitter-kotlin@v0.3.2
cp "$KT/src/parser.c" "$KT/src/scanner.c" third_party/tree-sitter-kotlin/src/
cp "$KT/src/tree_sitter/"*.h third_party/tree-sitter-kotlin/src/tree_sitter/
```
