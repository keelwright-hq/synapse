package parse

import "github.com/keelwright-hq/synapse/internal/graph"

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
	KindDoc       = "doc"     // Markdown / ADR document node (Phase 3)
	KindHeading   = "heading" // Doc section heading (Phase 3)
)

// Edge types emitted by extractors and contract binders.
const (
	EdgeContains   graph.EdgeType = "contains"
	EdgeImports    graph.EdgeType = "imports"
	EdgeCalls      graph.EdgeType = "calls"
	EdgeImplements graph.EdgeType = "implements"
	EdgeConsumes   graph.EdgeType = "consumes"
	// Phase 3 semantic edge families (SYN-3).
	EdgeCoCommitted   graph.EdgeType = "co_committed"   // git history co-change (HISTORICAL)
	EdgeObservedCalls graph.EdgeType = "observed_calls" // OpenTelemetry runtime calls (OBSERVED)
	EdgeDocuments     graph.EdgeType = "documents"      // doc → symbol / path reference
)

// Edge property keys and provenance values for Phase 3 edges.
const (
	PropProvenance = "provenance"
	PropWeight     = "weight"
	PropSupport    = "support"
	PropCount      = "count"
	PropLastSeen   = "last_seen"
	PropDoc        = "doc" // godoc / TSDoc / JSDoc text on a symbol node

	ProvenanceExtracted  = "EXTRACTED"
	ProvenanceInferred   = "INFERRED"
	ProvenanceHistorical = "HISTORICAL"
	ProvenanceObserved   = "OBSERVED"
)
