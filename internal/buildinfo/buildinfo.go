// Package buildinfo holds version metadata injected at link time via ldflags.
package buildinfo

// These defaults are overridden by -ldflags at build time:
//
//	-X github.com/taricsa/synapse/internal/buildinfo.Version=...
//	-X github.com/taricsa/synapse/internal/buildinfo.Commit=...
//	-X github.com/taricsa/synapse/internal/buildinfo.Date=...
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
