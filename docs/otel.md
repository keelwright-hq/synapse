# OpenTelemetry runtime ingestion (SYN-19)

Synapse can ingest runtime spans as `observed_calls` edges (provenance
`OBSERVED`), distinct from static `calls` edges.

## Trace file format

```json
{
  "spans": [
    {
      "name": "main",
      "service": "api",
      "attributes": {
        "code.function": "main",
        "code.filepath": "cmd/api/main.go"
      }
    },
    {
      "name": "helper",
      "parent": "main",
      "attributes": {
        "code.function": "helper"
      }
    }
  ]
}
```

Parent/child span names within the file create `observed_calls` edges when both
ends resolve to indexed symbols.

## CLI

```bash
synapse index . --repo myrepo
synapse otel ingest ./traces/sample.json --repo myrepo --data-dir .synapse
```

Re-run ingest after re-indexing (file-owned nodes are replaced on index).

## Collector sketch

Export spans to JSON (file exporter) and point `synapse otel ingest` at the
file, or convert OTLP JSON to the schema above. A live OTLP HTTP receiver can
be added later; the file importer is the supported path for Phase 3.
