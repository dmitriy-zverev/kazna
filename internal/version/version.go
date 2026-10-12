package version

import "runtime/debug"

// These values are overridden at build time via -ldflags (see Makefile / Dockerfile).
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

// String returns a compact human-readable build identifier.
func String() string {
	return Version + " (" + Commit + ")"
}

// Revision reports the module revision when built from a VCS checkout.
func Revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Commit
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && Commit == "none" {
			return s.Value
		}
	}
	return Commit
}
