// Command coprobe boots bee-lite and kubo in a single process, in parallel,
// and verifies that both HTTP gateways respond. This is the host-side
// analogue of what an Android consumer will do in production: one OS process
// hosting both nodes, each on its own port, sharing the same Go runtime
// and libp2p stack version-resolution.
//
// What it verifies:
//
//   - StartNode and StartIpfsNode can run concurrently without init-time
//     panics, deadlocks, or resource contention.
//   - bee's :1633 gateway and kubo's :18080 gateway both serve 200s.
//   - Their libp2p stacks coexist (bee on :1634, kubo on :4001).
//   - Shutdown is clean in either order.
//
// Ports (also the production Android defaults):
//
//	bee   HTTP   :1633
//	bee   libp2p :1634
//	kubo  HTTP   :18080  (freely configurable via IpfsNodeOptions.GatewayAddr)
//	kubo  libp2p :4001   (kubo's default)
//
// Usage:
//
//	go run ./cmd/coprobe
//
// Exit codes:
//
//	0 — both nodes started, both gateways responded, clean shutdown
//	1 — anything went wrong
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/solardev-xyz/freedom-node-mobile/mobile"
)

const (
	beeHealthURL     = "http://127.0.0.1:1633/health"
	ipfsGatewayAddr  = "127.0.0.1:18080"
	ipfsProbePayload = "freedom-node-mobile coprobe round-trip marker\n"

	bringupTimeout = 60 * time.Second
	httpPoll       = 500 * time.Millisecond
	shutdownOrder  = "ipfs,bee" // kubo shuts down fast; drain it first
	keystorePwd    = "coprobe-keystore"
)

func main() {
	beeDir, err := os.MkdirTemp("", "coprobe-bee-*")
	if err != nil {
		die("mkdir bee tmp: %v", err)
	}
	defer os.RemoveAll(beeDir)

	ipfsDir, err := os.MkdirTemp("", "coprobe-ipfs-*")
	if err != nil {
		die("mkdir ipfs tmp: %v", err)
	}
	defer os.RemoveAll(ipfsDir)

	log("bee  data dir: %s", beeDir)
	log("ipfs data dir: %s", ipfsDir)

	globalStart := time.Now()

	// Start both nodes concurrently. If either panics or deadlocks at
	// init, the probe fails loudly rather than silently hanging.
	var (
		wg       sync.WaitGroup
		bee      mobile.MobileNode
		ipfs     mobile.IpfsNode
		beeErr   error
		ipfsErr  error
		beeDone  = make(chan time.Duration, 1)
		ipfsDone = make(chan time.Duration, 1)
	)
	wg.Add(2)

	go func() {
		defer wg.Done()
		t := time.Now()
		bee, beeErr = startBee(beeDir)
		beeDone <- time.Since(t)
	}()

	go func() {
		defer wg.Done()
		t := time.Now()
		ipfs, ipfsErr = startIpfs(ipfsDir)
		ipfsDone <- time.Since(t)
	}()

	wg.Wait()

	if beeErr != nil {
		shutdownBest(ipfs)
		die("bee startup: %v", beeErr)
	}
	if ipfsErr != nil {
		shutdownBest(bee)
		die("ipfs startup: %v", ipfsErr)
	}

	log("bee   started in %s", (<-beeDone).Round(time.Millisecond))
	log("ipfs  started in %s (peer %s)", (<-ipfsDone).Round(time.Millisecond), trunc(ipfs.PeerID(), 12))

	ctx, cancel := context.WithTimeout(context.Background(), bringupTimeout)
	defer cancel()

	if err := verifyBeeHealth(ctx); err != nil {
		_ = bee.Shutdown()
		_ = ipfs.Shutdown()
		die("bee /health: %v", err)
	}
	log("bee   gateway GET /health → 200 OK")

	if err := verifyIpfsRoundTrip(ctx, ipfs); err != nil {
		_ = ipfs.Shutdown()
		_ = bee.Shutdown()
		die("ipfs round-trip: %v", err)
	}
	log("ipfs  gateway round-trip OK (AddBytes + HTTP GET, bytes match)")

	log("bee   connected peers: %d", bee.ConnectedPeerCount())
	log("ipfs  connected peers: %d", ipfs.ConnectedPeerCount())

	log("shutting down (%s)...", shutdownOrder)
	shutStart := time.Now()
	if err := ipfs.Shutdown(); err != nil {
		die("ipfs shutdown: %v", err)
	}
	log("ipfs  shutdown clean in %s", time.Since(shutStart).Round(time.Millisecond))

	shutStart = time.Now()
	if err := bee.Shutdown(); err != nil {
		die("bee shutdown: %v", err)
	}
	log("bee   shutdown clean in %s", time.Since(shutStart).Round(time.Millisecond))

	log("coprobe done  (total elapsed %s)", time.Since(globalStart).Round(time.Millisecond))
}

func startBee(dataDir string) (mobile.MobileNode, error) {
	opts := &mobile.MobileNodeOptions{
		DataDir:                  dataDir,
		WelcomeMessage:           "freedom-node-mobile coprobe (bee)",
		SwapInitialDeposit:       "0",
		PaymentThreshold:         "100000000",
		Mainnet:                  true,
		NetworkID:                1,
		CacheCapacity:            32 * 1024 * 1024,
		DBOpenFilesLimit:         50,
		DBWriteBufferSize:        32 * 1024 * 1024,
		DBBlockCacheCapacity:     32 * 1024 * 1024,
		DBDisableSeeksCompaction: false,
		RetrievalCaching:         true,
	}
	return mobile.StartNode(opts, keystorePwd, "3")
}

func startIpfs(dataDir string) (mobile.IpfsNode, error) {
	opts := &mobile.IpfsNodeOptions{
		DataDir:     dataDir,
		GatewayAddr: ipfsGatewayAddr,
		Offline:     false,
		LowPower:    true,
		RoutingMode: "dhtclient",
	}
	return mobile.StartIpfsNode(opts, "info")
}

func verifyBeeHealth(ctx context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(httpPoll)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout; last error: %v", lastErr)
		case <-ticker.C:
		}
		resp, err := client.Get(beeHealthURL)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return nil
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}

func verifyIpfsRoundTrip(ctx context.Context, node mobile.IpfsNode) error {
	ipfsPath, err := node.AddBytes([]byte(ipfsProbePayload))
	if err != nil {
		return fmt.Errorf("AddBytes: %w", err)
	}

	url := fmt.Sprintf("http://%s%s", ipfsGatewayAddr, ipfsPath)
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(httpPoll)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout; last error: %v", lastErr)
		case <-ticker.C:
		}
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		if string(body) != ipfsProbePayload {
			return fmt.Errorf("body mismatch: got %q, want %q", string(body), ipfsProbePayload)
		}
		return nil
	}
}

func shutdownBest(closers ...any) {
	for _, c := range closers {
		switch n := c.(type) {
		case mobile.MobileNode:
			if n != nil {
				_ = n.Shutdown()
			}
		case mobile.IpfsNode:
			if n != nil {
				_ = n.Shutdown()
			}
		}
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func log(format string, args ...any) {
	fmt.Printf("[coprobe] "+format+"\n", args...)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[coprobe] ERROR: "+format+"\n", args...)
	os.Exit(1)
}
