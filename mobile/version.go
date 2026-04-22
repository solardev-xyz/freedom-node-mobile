package mobile

// version is overridden at build time via
// -ldflags "-X github.com/solardev-xyz/freedom-node-mobile/mobile.version=<tag>".
// When built outside of `make build` (e.g. plain `go build`, `go test`, or a
// probe run without LDFLAGS), it stays at "dev" so we never panic or lie.
var version = "dev"

// Version returns the build-time version string, typically the output of
// `git describe --tags --dirty --always` captured at make-time. Exported
// through gomobile as mobile.Mobile.version() on the Java side so Android
// callers can surface it in about screens or bug reports.
func Version() string {
	return version
}
