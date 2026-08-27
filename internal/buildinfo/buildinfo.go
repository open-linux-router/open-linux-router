// Package buildinfo carries version metadata stamped in at link time.
package buildinfo

// Overridden via -ldflags -X. See the Makefile.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
