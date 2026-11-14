// Package version holds build-time version information.
package version

// Set via -ldflags "-X github.com/bolgov0zero/linager/internal/version.Version=...".
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
