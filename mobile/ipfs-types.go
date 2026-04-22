package mobile

// IpfsNodeOptions is the configuration surface for StartIpfsNode. It mirrors
// bee-lite's MobileNodeOptions idiom: only gomobile-representable scalar
// fields, with any "list" fields encoded as StringSliceDelimiter-separated
// strings.
type IpfsNodeOptions struct {
	// DataDir is the path to the kubo repo. If the directory does not
	// exist or is uninitialized, StartIpfsNode creates a default repo
	// there before opening it.
	DataDir string

	// GatewayAddr is the TCP listen address for the HTTP gateway in
	// Go net.Listen "host:port" form (e.g. "127.0.0.1:8080"). Empty
	// disables the gateway. M3 starts the gateway but does not yet
	// coordinate port allocation with bee; M4 handles that.
	GatewayAddr string

	// Offline starts the node without dialing peers or running the
	// libp2p host. Useful for tests and for host-probe runs that don't
	// want to touch the network. Production mobile usage leaves this
	// false.
	Offline bool

	// LowPower configures libp2p with reduced connection/stream limits
	// and disables the DHT server. Matches bee's ultra-light mode in
	// intent: run an IPFS node that's cheap on battery and bandwidth.
	LowPower bool

	// RoutingMode selects the IPFS content routing strategy:
	//   - "dht"     — full DHT participation (default, higher power)
	//   - "dhtclient" — DHT lookups only, do not serve lookups to peers
	//   - "autoclient" — delegated routing + light DHT client
	//   - "none"    — no content routing (useful for gateway-only nodes)
	// Empty falls back to the kubo default ("dht").
	RoutingMode string
}

// IpfsStats is a snapshot of runtime state. Kept intentionally small; fields
// can be added in future milestones without breaking the existing
// Java-visible shape since gomobile binds structs as final classes with
// per-field accessors.
type IpfsStats struct {
	PeerID         string
	ConnectedPeers int
	GatewayAddr    string
}
