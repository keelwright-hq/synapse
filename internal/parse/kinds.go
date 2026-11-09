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
	// Type relationships resolved at index time (Swift today).
	EdgeInherits   graph.EdgeType = "inherits"   // type → supertype or conformed protocol
	EdgeReferences graph.EdgeType = "references" // declaration → type it uses in a signature or body
)

// Edge property keys and provenance values for Phase 3 edges.
const (
	PropProvenance = "provenance"
	PropWeight     = "weight"
	PropSupport    = "support"
	PropCount      = "count"
	PropLastSeen   = "last_seen"
	PropDoc        = "doc" // godoc / TSDoc / JSDoc text on a symbol node
	// PropExtension marks a type node that only extends a type declared
	// elsewhere (Swift `extension Foo`), so it is not that type's definition.
	PropExtension = "extension"
	// PropTypeRefs and PropSupertypes hold comma-separated type names a
	// declaration uses or inherits from; the index resolves them to type nodes.
	PropTypeRefs   = "type_refs"
	PropSupertypes = "supertypes"
	// PropVarTypes holds "name:Type" pairs for the parameters, locals and
	// stored properties a declaration gives a type, so a call through one
	// (`store.save()`) can be resolved against that type's methods.
	PropVarTypes = "var_types"
	// PropRequirement marks a method a protocol declares without a body.
	PropRequirement = "requirement"

	ProvenanceExtracted  = "EXTRACTED"
	ProvenanceInferred   = "INFERRED"
	ProvenanceHistorical = "HISTORICAL"
	ProvenanceObserved   = "OBSERVED"
)
