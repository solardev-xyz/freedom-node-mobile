# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-04-22

Initial public release. Single Android AAR embedding both a Swarm (bee) and
an IPFS (kubo) node, runnable in the same process — working around gomobile's
one-`classes.jar`-per-APK limitation.

### Added

- gomobile-bound Android AAR (`mobile-<version>.aar`) with both bee-lite and
  kubo Go runtimes linked into a single `libgojni.so` per ABI.
- Go API surface in `mobile/`:
  - **Swarm** (unchanged from bee-lite-java, drop-in compatible):
    `StartNode`, `MobileNode`, `MobileNodeOptions`, `BlockchainData`, `File`,
    `FileUploadResult`, `StampManager`, `UploadManager`,
    `ReadableBandwidthStats`.
  - **IPFS** (new): `StartIpfsNode`, `IpfsNode`, `IpfsNodeOptions`,
    `IpfsStats`. Supports offline, low-power, and four routing modes
    (`dht`, `dhtclient`, `autoclient`, `none`).
  - `Version()` — returns the build-time version string, injected via
    `-ldflags -X` from `git describe --tags --dirty --always`.
- Host-side probes, no Android toolchain required:
  - `make probe` — boots bee-lite, hits `/health`, shuts down.
  - `make probe-ipfs` — boots kubo online, round-trips bytes through
    CoreAPI and the HTTP gateway, shuts down.
  - `make probe-ipfs-offline` — same, without libp2p bring-up (~1 s,
    CI-friendly).
  - `make probe-co` — boots bee AND kubo concurrently in one process,
    verifies both gateways respond.
- `make verify` — structural sanity check on the built AAR: confirms the
  expected ABIs, the bound Java surface, and that bee, kubo, libp2p, and
  go-ethereum symbols are all linked into `libgojni.so`.
- Autodetection for `ANDROID_HOME`, `JAVA_HOME` (preferring JDK 17), and
  `$(go env GOPATH)/bin` in the Makefile, so `make install && make build`
  works on a fresh macOS / Linux checkout without editing shell rc files.

### Licensing

- Distributed under the Mozilla Public License 2.0 (matches
  [freedom-browser](https://github.com/solardev-xyz/freedom-browser)).
- `NOTICE` enumerates the four upstream node licenses we bundle:
  bee-lite (Apache-2.0), ethersphere/bee (BSD-3-Clause), kubo (MIT /
  Apache-2.0 dual), go-ethereum (LGPL-3.0 for the library portion).

### Known limitations

- **Android only.** The Go layer is platform-neutral, but no iOS
  `.xcframework` target exists and bee+kubo coexistence under iOS memory
  and background-execution constraints has not been validated.
- **Not byte-reproducible.** `gomobile bind` embeds the Go toolchain
  version in `classes.jar` and `go mod tidy` re-resolves transitive
  versions against the live module proxy; two builds on different days
  are functionally equivalent but produce different SHA-256s. Same
  behaviour inherited from bee-lite-java.
- **Android device memory pressure untested.** Probes confirm the Go
  layer is sound; on-device validation (including the `LowPower` +
  `autoclient` profile path for 4 GB devices) is deferred to the
  consuming Android app's integration suite.

[Unreleased]: https://github.com/solardev-xyz/freedom-node-mobile/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/solardev-xyz/freedom-node-mobile/releases/tag/v0.1.0
