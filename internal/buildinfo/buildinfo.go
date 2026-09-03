// Package buildinfo holds version metadata injected at link time via ldflags.
// When ldflags are unset (e.g. `go install …@latest`), values fall back to
// runtime/debug.ReadBuildInfo() so `synapse version` is still useful.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// These defaults are overridden by -ldflags at build time:
//
//	-X github.com/keelwright-hq/synapse/internal/buildinfo.Version=...
//	-X github.com/keelwright-hq/synapse/internal/buildinfo.Commit=...
//	-X github.com/keelwright-hq/synapse/internal/buildinfo.Date=...
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func init() {
	fillFromBuildInfo(debug.ReadBuildInfo)
}

func fillFromBuildInfo(read func() (*debug.BuildInfo, bool)) {
	bi, ok := read()
	if !ok || bi == nil {
		return
	}

	if Version == "dev" || Version == "" {
		if v := strings.TrimSpace(bi.Main.Version); v != "" && v != "(devel)" {
			Version = v
		}
	}

	var revision, modified, vcsTime string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		case "vcs.time":
			vcsTime = s.Value
		}
	}

	if (Commit == "none" || Commit == "") && revision != "" {
		commit := revision
		if len(commit) > 7 {
			commit = commit[:7]
		}
		if modified == "true" {
			commit += "-dirty"
		}
		Commit = commit
	}

	if (Date == "unknown" || Date == "") && vcsTime != "" {
		Date = vcsTime
	}
}
