// Package buildinfo carries version metadata stamped in at link time.
package buildinfo

import "fmt"

// Overridden via -ldflags -X. See the Makefile.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String renders the version without a binary name, so that olr and olrd report
// themselves identically apart from what they are called.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
