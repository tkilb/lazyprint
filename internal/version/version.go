// Package version holds the lazyprint build version. Version defaults to
// "dev" for local `go build`/`go run` and is overridden at release-build
// time via `-ldflags "-X github.com/tkilb/lazyprint/internal/version.Version=X.Y.Z"`.
package version

// Version is the lazyprint release version, e.g. "0.0.1".
var Version = "dev"
