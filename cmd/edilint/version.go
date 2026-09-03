package main

import (
	"runtime/debug"
	"strings"
)

// version is overridden at build time with -ldflags "-X main.version=...",
// which is how a goreleaser build gets its number. buildVersion is what the
// program reports; it consults the module build information when the linker
// left this at its default.
var version = "dev"

// buildVersion returns the version to report.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	return versionFrom(version, info, ok)
}

// versionFrom derives the reported version from the linker-set value and the
// module build information.
//
// A binary installed with "go install ...@v0.3.0" is built without ldflags,
// yet its module version is recorded in the build information, so it reports
// "0.3.0" rather than "dev". The leading "v" is dropped to match what
// goreleaser sets. A build from a checkout records no module version but
// usually records the commit, so "dev" is qualified with it, and with
// "-dirty" when the tree had uncommitted changes.
func versionFrom(set string, info *debug.BuildInfo, ok bool) string {
	if set != "dev" || !ok || info == nil {
		return set
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return strings.TrimPrefix(v, "v")
	}
	var revision string
	modified := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return set
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		revision += "-dirty"
	}
	return set + " (" + revision + ")"
}
