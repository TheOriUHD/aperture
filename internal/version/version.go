// Package version carries build metadata, injected at link time via -ldflags.
package version

var (
	// Version is the semantic version of the build.
	Version = "0.0.0-dev"
	// Commit is the short git SHA of the build.
	Commit = "none"
	// BuildTime is an RFC3339 timestamp of the build, if set.
	BuildTime = ""
)

// String returns a compact human-readable version string.
func String() string {
	s := Version
	if Commit != "" && Commit != "none" {
		s += " (" + Commit + ")"
	}
	return s
}
