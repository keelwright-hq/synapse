package parse

import "github.com/taricsa/synapse/internal/graph"

// Node kinds emitted by extractors.
const (
	KindFile     = "file"
	KindPackage  = "package"
	KindModule   = "module"
	KindFunction = "function"
	KindMethod   = "method"
	KindType     = "type"
	KindImport   = "import"
	KindSymbol   = "symbol"
)

// Edge types emitted by extractors.
const (
	EdgeContains graph.EdgeType = "contains"
	EdgeImports  graph.EdgeType = "imports"
	EdgeCalls    graph.EdgeType = "calls"
)
