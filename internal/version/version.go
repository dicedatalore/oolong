// Package version reports the app version shown in the banner and by the
// -version flag.
package version

import "runtime/debug"

// Version is stamped with the release tag at build time via -ldflags -X;
// it is empty for source builds.
var Version = ""

// String returns the stamped release version, falling back to the module
// version recorded in build info (set by `go install module@version`) and
// then to "dev" for local builds.
func String() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
