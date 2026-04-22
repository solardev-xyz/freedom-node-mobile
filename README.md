# freedom-node-mobile

Combined gomobile binding that bundles the [bee-lite](https://github.com/Solar-Punk-Ltd/bee-lite) Swarm node and [kubo](https://github.com/ipfs/kubo) IPFS node into a single `mobile.aar` consumable from an Android app.

Two gomobile-bound AARs cannot coexist in one Android APK: they both ship `classes.jar` with Java classes under the `mobile.*` / `go.*` packages, and both ship `libgojni.so` per ABI. This repo sidesteps that by producing **one** AAR where both runtimes live in the same Go binary.

## Platforms

Today this produces an **Android AAR only** (`gomobile bind -target=android`). The Go layer in `mobile/` is platform-neutral, so an iOS `.xcframework` is technically feasible via `gomobile bind -target=ios`, but hasn't been attempted here and will almost certainly need real engineering to fit inside iOS's per-process memory limits and background-execution rules (bee alone is a non-trivial RAM consumer; running it alongside kubo on an iPhone is not a given). The "mobile" in the name is aspirational on the iOS axis; treat this repo as the Android build for now.

## Requirements

| Component | Version | Notes |
|---|---|---|
| Go | 1.26+ | Matches `go 1.26.2` directive. |
| JDK | 17 | gomobile invokes `javac -source 1.8 -target 1.8` to compile the generated Java stubs; JDK 21+ rejects those source levels. On macOS: `brew install openjdk@17`. The Makefile autodetects the usual install locations; override with `JAVA_HOME=... make build`. |
| Android SDK | — | Any reasonable recent SDK with an NDK installed side-by-side under `$ANDROID_HOME/ndk/<version>/`. The Makefile autodetects common macOS / Linux install locations; override with `ANDROID_HOME=... make build`. |
| Android NDK | r27+ | Install via `sdkmanager "ndk;27.0.12077973"` (or newer point release; `r27.2` also works). |
| gomobile | latest | `make install` handles it. |

## Layout

```
freedom-node-mobile/
├── mobile/              # gomobile-visible package; becomes Java `mobile.*`
│   ├── mobile-wrapper.go  # bee: StartNode / MobileNode / MobileNodeOptions
│   ├── types.go
│   ├── stamp-manager.go
│   ├── upload.go
│   ├── ipfs-wrapper.go    # kubo: StartIpfsNode / IpfsNode / IpfsNodeOptions
│   └── ipfs-types.go
├── cmd/
│   ├── hostprobe/       # boots bee, curls /health, shuts down
│   ├── ipfsprobe/       # boots kubo, round-trips bytes via gateway, shuts down
│   └── coprobe/         # boots bee + kubo in one process, verifies both
├── stubs/
│   └── genproto/        # empty replace target for google.golang.org/genproto
├── go.mod
├── Makefile
└── README.md
```

The bee half of `mobile/` mirrors `Solar-Punk-Ltd/bee-lite-java` verbatim — preserving its `StartNode() / MobileNode / MobileNodeOptions` surface — so the resulting AAR is a drop-in replacement for the current one. The kubo half is added alongside as extra top-level symbols; it does not replace or rename anything bee exports.

## Quick start

```bash
# One-time: install gomobile + init.
make install

# Host-side smoke tests. No Android toolchain needed.
make probe              # bee: boot, /health, shutdown
make probe-ipfs         # kubo: boot, gateway round-trip via CoreAPI, shutdown
make probe-ipfs-offline # kubo without libp2p bring-up (~1s, CI-friendly)
make probe-co           # bee + kubo co-hosted in one process; both gateways respond
make probe-all          # all three probes in sequence

# Produce mobile.aar (~5 minutes cold, cache-fast after).
make build
ls -lh build/mobile.aar

# Confirm the AAR is well-formed: expected ABIs, Java surface bound,
# both bee and kubo symbols actually linked into libgojni.so.
make verify
```

To use the resulting AAR in an Android project, copy `build/mobile.aar` into the project's `libs/` directory and wire it up as a Gradle file dependency.

## Verifying the build

Three layers of confidence, from cheapest to strongest:

1. **Host-side probes** (`make probe`, `make probe-ipfs`, `make probe-co`) exercise the Go layer end-to-end without ever producing an AAR. Each probe boots the real node, hits the real gateway, and shuts down cleanly. `probe-co` in particular boots bee and kubo concurrently in one process — same configuration the AAR runs on-device — so a green `probe-co` is the strongest correctness signal available without an Android runtime.
2. **Structural check on the AAR** (`make verify`) confirms `build/mobile.aar` has all four ABIs, classes.jar contains the expected `mobile.Mobile` / `MobileNode` / `IpfsNode` surface, and `libgojni.so` contains symbols from bee, kubo, libp2p, and go-ethereum. Runs in ~2 seconds and needs only `unzip` + `strings`. Catches silent gomobile bind regressions (e.g. a rename that drops `IpfsNode` from the Java side) and link failures (e.g. a dependency refactor that accidentally strips kubo).
3. **On-device smoke test.** The only check `make verify` can't do is prove JNI marshalling works; that requires loading the AAR into an Android process. Typically done by the consuming app's integration test suite.

## API

The Go side of `mobile/` exports the bee-lite surface (`StartNode`, `MobileNode`, `MobileNodeOptions`, `BlockchainData`, `File`, `FileUploadResult`, etc. — unchanged from bee-lite-java) plus a parallel IPFS surface:

```go
func StartIpfsNode(options *IpfsNodeOptions, verbosity string) (IpfsNode, error)

type IpfsNode interface {
    Shutdown() error
    Stats() *IpfsStats
    PeerID() string
    ConnectedPeerCount() int
    GatewayAddr() string
    AddBytes(content []byte) (string, error)
}

type IpfsNodeOptions struct {
    DataDir     string
    GatewayAddr string
    Offline     bool
    LowPower    bool
    RoutingMode string  // "dht" | "dhtclient" | "autoclient" | "none"
}

type IpfsStats struct {
    PeerID         string
    ConnectedPeers int
    GatewayAddr    string
}
```

gomobile binds both surfaces into `mobile.*` under one `classes.jar`:

```java
public abstract class mobile.Mobile {
  public static native mobile.MobileNode startNode(mobile.MobileNodeOptions, String, String) throws Exception;
  public static native mobile.IpfsNode   startIpfsNode(mobile.IpfsNodeOptions, String) throws Exception;
  // ...
}

public interface mobile.IpfsNode {
  public String addBytes(byte[]) throws Exception;
  public long   connectedPeerCount();
  public String gatewayAddr();
  public String peerID();
  public void   shutdown() throws Exception;
  public mobile.IpfsStats stats();
}
```

## Ports

| Node | Purpose | Listen addr | Source |
|---|---|---|---|
| bee  | HTTP gateway / API | `:1633` | hardcoded in `bee-lite` |
| bee  | libp2p             | `:1634` | hardcoded in `bee-lite` |
| kubo | HTTP gateway       | configurable via `IpfsNodeOptions.GatewayAddr` (coprobe uses `:18080`) | our wrapper |
| kubo | libp2p             | `:4001` | kubo default from `config.Init` |

No collisions. If a consumer needs to change bee's `:1633`, that's a bee-lite-java change; kubo's ports are under our control via `IpfsNodeOptions`.

## Design notes

### `btcsuite/btcd` exclude

`go.mod` inherits this from bee-lite-java:

```
exclude github.com/btcsuite/btcd/chaincfg/chainhash v1.0.1
```

`btcd/chaincfg/chainhash` was later extracted into its own module. Both versions define the same import path, so without the exclude `go mod tidy` errors with an ambiguous-import. Keep this line.

### `google.golang.org/genproto` stub replace

`go.mod` redirects the monolithic `google.golang.org/genproto` module to an empty stub at `./stubs/genproto`:

```
replace google.golang.org/genproto => ./stubs/genproto
```

Context: the monolithic module was split into per-API submodules (`googleapis/api`, `googleapis/rpc`, ...) in 2023, but the old subtrees still live inside the monolithic module. Our combined graph pulls in both — bee-lite's transitive cloud SDKs still require monolithic versions, kubo uses only the split submodules — which makes `googleapis/api/annotations`, `googleapis/rpc/status`, and friends ambiguous at build time (the same Go import path is served by two modules). The stub removes the monolithic side's claim to those paths so only the split submodules provide them. No production code actually needs the monolithic paths; the only references are test fixtures in `grpc-gateway/v2/runtime/internal/examplepb`, which we never compile.

### `-checklinkname=0`

bee and `go-ethereum` use `//go:linkname` into the Go runtime to access private symbols. Since Go 1.23 the linker rejects this by default; `-checklinkname=0` opts out. kubo and `go-libp2p` also need it. Applied consistently in `Makefile` for all build targets.

### Coexistence in one process

`cmd/coprobe` boots bee and kubo concurrently in one process and exercises both gateways. That rules out:

- **libp2p version conflicts.** Bee and kubo depend on different `go-libp2p` branches but MVS picks a single version; if the resolved version were incompatible with either at runtime, coprobe would panic.
- **`go:linkname` collisions.** Both stacks lean on `-checklinkname=0`-requiring symbols and coexist in one binary without cross-contamination.
- **Double-init of shared plugin registries.** Kubo's plugin loader uses process-global state (IPLD codec registry, datastore plugins). Our wrapper guards it with `sync.Once`; coprobe re-runs back-to-back without crashing.
- **Port contention.** Bee `:1633/:1634`, kubo `:18080/:4001` — no overlap.
- **Shutdown ordering.** Both orders (ipfs→bee and bee→ipfs) shut down cleanly; coprobe uses ipfs→bee because kubo's teardown (~15 ms) is much faster than bee's (~130 ms).

### Reproducibility

`gomobile bind`'s output is not byte-stable: `go mod tidy` re-resolves transitive dep versions against the live module proxy, and `gomobile bind` embeds the Go toolchain version in the resulting `classes.jar`. Two builds on different days are functionally equivalent but produce different SHA-256s. Same behaviour as bee-lite-java.

## Caveats

Host-side probes cover the Go layer end-to-end but not memory pressure on a real Android device. Phones with 4 GB RAM will likely need the `LowPower` profile plus the `autoclient` routing mode, both wired through `IpfsNodeOptions`.

## License

[Mozilla Public License 2.0](./LICENSE). Upstream node licenses (bee-lite, Swarm bee, kubo, go-ethereum) are enumerated in [NOTICE](./NOTICE).
