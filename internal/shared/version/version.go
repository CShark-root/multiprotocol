// Package version reports the build version, set at link time with
// -ldflags "-X panel/internal/shared/version.Version=v0.1.0".
package version

var Version = "dev"
