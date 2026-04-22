// Empty stub for google.golang.org/genproto.
//
// The monolithic `google.golang.org/genproto` module was split in 2023
// into per-API submodules (`googleapis/api`, `googleapis/rpc`, ...). We
// use both `bee-lite` (which transitively pins old versions of cloud
// SDKs requiring the monolithic genproto) and `kubo` (which targets the
// split submodules). MVS selects both, and the `googleapis/api/*` and
// `googleapis/rpc/*` paths exist in both module trees, producing
// ambiguous-import errors at build time.
//
// By `replace`-ing the monolithic genproto with this empty stub, we
// strip those overlapping paths from the monolithic side, leaving the
// split submodules as the sole providers. Real production code targets
// the split paths; the only references to the monolithic paths are
// test fixtures in grpc-gateway's `runtime/internal/examplepb`, which
// we neither compile nor test.
module google.golang.org/genproto

go 1.21
