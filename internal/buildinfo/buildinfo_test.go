package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFillFromBuildInfoOverridesDefaults(t *testing.T) {
	origV, origC, origD := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = origV, origC, origD
	})

	Version, Commit, Date = "dev", "none", "unknown"
	fillFromBuildInfo(func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v1.2.3"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef0123456789"},
				{Key: "vcs.modified", Value: "false"},
				{Key: "vcs.time", Value: "2026-09-03T12:00:00Z"},
			},
		}, true
	})

	if Version != "v1.2.3" {
		t.Fatalf("Version=%q want v1.2.3", Version)
	}
	if Commit != "abcdef0" {
		t.Fatalf("Commit=%q want abcdef0", Commit)
	}
	if Date != "2026-09-03T12:00:00Z" {
		t.Fatalf("Date=%q", Date)
	}
}

func TestFillFromBuildInfoPreservesLdflags(t *testing.T) {
	origV, origC, origD := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = origV, origC, origD
	})

	Version, Commit, Date = "v9.9.9", "deadbee", "stamp"
	fillFromBuildInfo(func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v1.2.3"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef0123456789"},
				{Key: "vcs.time", Value: "2026-09-03T12:00:00Z"},
			},
		}, true
	})

	if Version != "v9.9.9" || Commit != "deadbee" || Date != "stamp" {
		t.Fatalf("ldflags overwritten: Version=%q Commit=%q Date=%q", Version, Commit, Date)
	}
}

func TestFillFromBuildInfoSkipsDevel(t *testing.T) {
	origV := Version
	t.Cleanup(func() { Version = origV })

	Version = "dev"
	fillFromBuildInfo(func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
	})
	if Version != "dev" {
		t.Fatalf("Version=%q want to keep dev for (devel)", Version)
	}
}
