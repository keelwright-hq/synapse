package parse

import "github.com/taricsa/synapse/internal/graph"

// Node kinds emitted by extractors and contract parsers.
const (
	KindFile      = "file"
	KindPackage   = "package"
	KindModule    = "module"
	KindFunction  = "function"
	KindMethod    = "method"
	KindType      = "type"
	KindImport    = "import"
	KindSymbol    = "symbol"
	KindOperation = "operation"
	KindSchema    = "schema"
	KindField     = "field"
	KindService   = "service"
)

// Edge types emitted by extractors and contract binders.
const (
	EdgeContains    graph.EdgeType = "contains"
	EdgeImports     graph.EdgeType = "imports"
	EdgeCalls       graph.EdgeType = "calls"
	EdgeImplements  graph.EdgeType = "implements"
	EdgeConsumes    graph.EdgeType = "consumes"
)
