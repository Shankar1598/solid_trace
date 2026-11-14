// Package version holds the version of this solidtrace binary.
package version

// Version is the release that this binary is, such as v0.19.0. A release
// build sets it with
// -ldflags "-X github.com/solidtrace/cli/version.Version=v0.19.0".
// A build from a checkout is "dev".
var Version = "dev"
