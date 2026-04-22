// Command ipfsprobe boots a kubo IPFS node on the host and verifies the
// HTTP gateway responds. Counterpart to cmd/hostprobe for the bee side;
// lets us validate the kubo half of the combined binary without Android.
//
// Usage:
//
//	go run ./cmd/ipfsprobe
//	go run ./cmd/ipfsprobe --offline   # skip libp2p / DHT bring-up
//
// Exit codes:
//
//	0 — node started, gateway returned 200 on a well-known CID, clean shutdown
//	1 — startup / gateway / shutdown failed
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/solardev-xyz/freedom-node-mobile/mobile"
)

const (
	gatewayAddr    = "127.0.0.1:18080"
	healthPoll     = 500 * time.Millisecond
	fetchTimeout   = 30 * time.Second
)

const probePayload = "freedom-node-mobile ipfsprobe round-trip marker\n"

var offline = flag.Bool("offline", false, "skip libp2p bring-up (still hits local blockstore)")

func main() {
	flag.Parse()

	dataDir, err := os.MkdirTemp("", "freedom-node-mobile-ipfsprobe-*")
	if err != nil {
		die("mkdir tmp: %v", err)
	}
	defer os.RemoveAll(dataDir)

	opts := &mobile.IpfsNodeOptions{
		DataDir:     dataDir,
		GatewayAddr: gatewayAddr,
		Offline:     *offline,
		LowPower:    true,
		RoutingMode: "dhtclient",
	}

	log("data dir:  %s", dataDir)
	log("offline:   %v", *offline)
	log("starting kubo node (lowpower, %s)...", opts.RoutingMode)

	startedAt := time.Now()
	node, err := mobile.StartIpfsNode(opts, "info")
	if err != nil {
		die("StartIpfsNode: %v", err)
	}
	log("node started in %s", time.Since(startedAt).Round(time.Millisecond))
	log("peer ID:   %s", node.PeerID())
	log("gateway:   http://%s", node.GatewayAddr())

	log("adding probe payload via CoreAPI...")
	ipfsPath, err := node.AddBytes([]byte(probePayload))
	if err != nil {
		_ = node.Shutdown()
		die("AddBytes: %v", err)
	}
	log("added:     %s", ipfsPath)

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	fetchURL := fmt.Sprintf("http://%s%s", gatewayAddr, ipfsPath)
	log("fetching   %s", fetchURL)
	body, err := fetchWithRetry(ctx, fetchURL)
	if err != nil {
		_ = node.Shutdown()
		die("gateway fetch: %v", err)
	}
	if string(body) != probePayload {
		_ = node.Shutdown()
		die("round-trip mismatch: got %q, want %q", string(body), probePayload)
	}
	log("gateway → 200 OK, body matches (%d bytes, round-trip verified)", len(body))

	if !*offline {
		log("connected peers: %d", node.ConnectedPeerCount())
	}

	log("shutting down...")
	if err := node.Shutdown(); err != nil {
		die("Shutdown: %v", err)
	}
	log("shutdown clean  (total elapsed %s)", time.Since(startedAt).Round(time.Millisecond))
}

func fetchWithRetry(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(healthPoll)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("timed out: %v", lastErr)
			}
			return nil, fmt.Errorf("timed out")
		case <-ticker.C:
		}

		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return body, nil
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}

func log(format string, args ...any) {
	fmt.Printf("[ipfsprobe] "+format+"\n", args...)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[ipfsprobe] ERROR: "+format+"\n", args...)
	os.Exit(1)
}
