package mobile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ipfs/boxo/files"
	kuboconfig "github.com/ipfs/kubo/config"
	kubocore "github.com/ipfs/kubo/core"
	"github.com/ipfs/kubo/core/coreapi"
	coreiface "github.com/ipfs/kubo/core/coreiface"
	"github.com/ipfs/kubo/core/coreiface/options"
	"github.com/ipfs/kubo/core/corehttp"
	"github.com/ipfs/kubo/core/node/libp2p"
	pluginloader "github.com/ipfs/kubo/plugin/loader"
	"github.com/ipfs/kubo/repo/fsrepo"
)

// IpfsNode is the gomobile-visible handle on a running kubo node. Intentionally
// a narrow interface: anything richer lives behind IpfsStats so the Java
// surface is stable across kubo upgrades.
type IpfsNode interface {
	Shutdown() error
	Stats() *IpfsStats
	PeerID() string
	ConnectedPeerCount() int
	GatewayAddr() string

	// AddBytes pins `content` into the local blockstore and returns the
	// resulting /ipfs/<cid> path. Useful for host-side round-trip
	// testing and for Android features that want to "save to IPFS"
	// without going through the HTTP API. The pin is local-only; it
	// does not provide the CID to the DHT.
	AddBytes(content []byte) (string, error)
}

type ipfsNodeImp struct {
	node *kubocore.IpfsNode
	api  coreiface.CoreAPI

	// When the gateway is enabled we own its listener; closing the
	// listener is what stops corehttp.Serve. We also track the serve
	// goroutine's exit so Shutdown can wait for a clean teardown.
	gwListener  net.Listener
	gwAddr      string
	gwServeDone <-chan struct{}
	gwServeErr  atomic.Value // error

	shutdownOnce sync.Once
	shutdownErr  error
}

// Global plugin loader state. Kubo's plugin system is process-global —
// Initialize/Inject mutate process-level registries (IPLD codecs, datastore
// plugins). On Android, StartIpfsNode may be called more than once across
// Service restarts; we only want to run plugin setup once per process.
var (
	pluginSetupOnce sync.Once
	pluginSetupErr  error
)

// StartIpfsNode initializes (if needed) a kubo repo at options.DataDir, opens
// it, builds an IpfsNode, and optionally starts the HTTP gateway. The
// returned IpfsNode is safe to use from any thread; call Shutdown exactly
// once when done.
//
// verbosity accepts the same vocabulary as bee-lite's StartNode so callers
// can route both nodes' logs uniformly, but kubo's logger is set via
// environment rather than per-node, so this currently only affects
// diagnostic prints emitted by this wrapper.
func StartIpfsNode(options *IpfsNodeOptions, verbosity string) (IpfsNode, error) {
	if options == nil {
		return nil, errors.New("IpfsNodeOptions is required")
	}
	if options.DataDir == "" {
		return nil, errors.New("IpfsNodeOptions.DataDir is required")
	}

	if err := setupPlugins(options.DataDir); err != nil {
		return nil, fmt.Errorf("plugin setup: %w", err)
	}

	if err := ensureRepoInitialized(options); err != nil {
		return nil, fmt.Errorf("repo init: %w", err)
	}

	repo, err := fsrepo.Open(options.DataDir)
	if err != nil {
		return nil, fmt.Errorf("fsrepo open: %w", err)
	}

	ctx := context.Background()
	buildCfg := &kubocore.BuildCfg{
		Online:  !options.Offline,
		Routing: routingOption(options.RoutingMode),
		Repo:    repo,
		ExtraOpts: map[string]bool{
			"pubsub": false,
			"ipnsps": false,
		},
	}

	node, err := kubocore.NewNode(ctx, buildCfg)
	if err != nil {
		_ = repo.Close()
		return nil, fmt.Errorf("core.NewNode: %w", err)
	}

	api, err := coreapi.NewCoreAPI(node)
	if err != nil {
		_ = node.Close()
		return nil, fmt.Errorf("coreapi.NewCoreAPI: %w", err)
	}

	imp := &ipfsNodeImp{node: node, api: api}

	if options.GatewayAddr != "" {
		if err := imp.startGateway(options.GatewayAddr); err != nil {
			_ = node.Close()
			return nil, fmt.Errorf("gateway: %w", err)
		}
	}

	return imp, nil
}

func setupPlugins(pluginPath string) error {
	pluginSetupOnce.Do(func() {
		plugins, err := pluginloader.NewPluginLoader(pluginPath)
		if err != nil {
			pluginSetupErr = fmt.Errorf("new loader: %w", err)
			return
		}
		if err := plugins.Initialize(); err != nil {
			pluginSetupErr = fmt.Errorf("initialize: %w", err)
			return
		}
		if err := plugins.Inject(); err != nil {
			pluginSetupErr = fmt.Errorf("inject: %w", err)
			return
		}
	})
	return pluginSetupErr
}

func ensureRepoInitialized(options *IpfsNodeOptions) error {
	if fsrepo.IsInitialized(options.DataDir) {
		return nil
	}

	if err := os.MkdirAll(options.DataDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", options.DataDir, err)
	}

	// 2048-bit RSA is the kubo default; fine for mobile where this cost
	// is paid once on first launch and then cached in the repo.
	cfg, err := kuboconfig.Init(io.Discard, 2048)
	if err != nil {
		return fmt.Errorf("config init: %w", err)
	}

	if options.LowPower {
		if profile, ok := kuboconfig.Profiles["lowpower"]; ok {
			if err := profile.Transform(cfg); err != nil {
				return fmt.Errorf("apply lowpower profile: %w", err)
			}
		}
	}

	if options.RoutingMode != "" {
		cfg.Routing.Type = kuboconfig.NewOptionalString(options.RoutingMode)
	}

	return fsrepo.Init(options.DataDir, cfg)
}

func routingOption(mode string) libp2p.RoutingOption {
	switch mode {
	case "dhtclient":
		return libp2p.DHTClientOption
	case "none":
		return libp2p.NilRouterOption
	default:
		// Empty, "dht", "auto", "autoclient" all fall through to the
		// default DHT option. "autoclient" is a repo-config-level
		// toggle; it's applied via cfg.Routing.Type at init, not via
		// the libp2p.RoutingOption we hand to BuildCfg.
		return libp2p.DHTOption
	}
}

func (i *ipfsNodeImp) startGateway(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	i.gwListener = listener
	i.gwAddr = listener.Addr().String()

	opts := []corehttp.ServeOption{
		corehttp.VersionOption(),
		corehttp.GatewayOption("/ipfs", "/ipns"),
	}

	ready := make(chan struct{})
	done := make(chan struct{})
	i.gwServeDone = done

	go func() {
		defer close(done)
		if err := corehttp.ServeWithReady(i.node, listener, ready, opts...); err != nil {
			i.gwServeErr.Store(err)
		}
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		_ = listener.Close()
		return errors.New("gateway did not signal ready within 5s")
	}

	return nil
}

func (i *ipfsNodeImp) Shutdown() error {
	i.shutdownOnce.Do(func() {
		if i.gwListener != nil {
			// Closing the listener triggers corehttp.Serve to return.
			// We still call node.Close below; it cancels the node
			// context which ServeWithReady also watches, giving a
			// clean tear-down either way.
			_ = i.gwListener.Close()
		}

		if err := i.node.Close(); err != nil {
			i.shutdownErr = err
			return
		}

		if i.gwServeDone != nil {
			select {
			case <-i.gwServeDone:
			case <-time.After(5 * time.Second):
				i.shutdownErr = errors.New("gateway did not shut down within 5s")
				return
			}
			if v := i.gwServeErr.Load(); v != nil {
				if err, ok := v.(error); ok && err != nil && !isExpectedShutdownErr(err) {
					i.shutdownErr = err
				}
			}
		}
	})
	return i.shutdownErr
}

func (i *ipfsNodeImp) PeerID() string {
	if i.node == nil || i.node.Identity == "" {
		return ""
	}
	return i.node.Identity.String()
}

func (i *ipfsNodeImp) ConnectedPeerCount() int {
	if i.node == nil || i.node.PeerHost == nil {
		return 0
	}
	return len(i.node.PeerHost.Network().Peers())
}

func (i *ipfsNodeImp) GatewayAddr() string {
	return i.gwAddr
}

// isExpectedShutdownErr recognizes the family of net errors that mean
// "the listener was closed on purpose". corehttp.ServeWithReady surfaces
// both the raw `net.ErrClosed` (wrapped in an *net.OpError) and
// http.ErrServerClosed depending on the race between listener.Close and
// node.Close. Neither is interesting to callers of Shutdown.
func isExpectedShutdownErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed)
}

func (i *ipfsNodeImp) AddBytes(content []byte) (string, error) {
	if i.api == nil {
		return "", errors.New("ipfs node is not initialized")
	}

	// Pin=true is the kubo default; CidVersion=1 gives CIDv1 / base32
	// output which plays nicer with DNS and URL schemes than the legacy
	// QmHash form.
	resolved, err := i.api.Unixfs().Add(
		i.node.Context(),
		files.NewBytesFile(content),
		options.Unixfs.Pin(true, "ipfsprobe"),
		options.Unixfs.CidVersion(1),
	)
	if err != nil {
		return "", err
	}
	return resolved.String(), nil
}

func (i *ipfsNodeImp) Stats() *IpfsStats {
	return &IpfsStats{
		PeerID:         i.PeerID(),
		ConnectedPeers: i.ConnectedPeerCount(),
		GatewayAddr:    i.gwAddr,
	}
}
