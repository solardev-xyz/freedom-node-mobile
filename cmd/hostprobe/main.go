// Command hostprobe boots the embedded bee node on the host and polls
// http://127.0.0.1:1633/health until it responds. Exercises the full
// lifecycle (start → HTTP gateway up → shutdown) without requiring any
// Android tooling. If this passes, the Go side of the combined module is
// behaving identically to bee-lite-java's upstream AAR.
//
// Usage:
//
//	go run ./cmd/hostprobe
//
// Exit codes:
//
//	0 — node started, /health returned 200, clean shutdown
//	1 — startup / health / shutdown failed
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/solardev-xyz/freedom-node-mobile/mobile"
)

const (
	healthURL     = "http://127.0.0.1:1633/health"
	healthTimeout = 45 * time.Second
	healthPoll    = 500 * time.Millisecond
	// Keystore password only protects the on-device wallet file bee-lite
	// generates at first start; in ultra-light mode the wallet is
	// read-only anyway, so this is a throwaway per-probe value.
	keystorePassword = "hostprobe-keystore"
)

func main() {
	dataDir, err := os.MkdirTemp("", "freedom-node-mobile-probe-*")
	if err != nil {
		die("mkdir tmp: %v", err)
	}
	defer os.RemoveAll(dataDir)

	opts := &mobile.MobileNodeOptions{
		FullNodeMode:             false,
		BootnodeMode:             false,
		Bootnodes:                "",
		DataDir:                  dataDir,
		WelcomeMessage:           "freedom-node-mobile hostprobe",
		BlockchainRpcEndpoint:    "",
		SwapInitialDeposit:       "0",
		PaymentThreshold:         "100000000",
		SwapEnable:               false,
		ChequebookEnable:         false,
		UsePostageSnapshot:       false,
		Mainnet:                  true,
		NetworkID:                1,
		CacheCapacity:            32 * 1024 * 1024,
		DBOpenFilesLimit:         50,
		DBWriteBufferSize:        32 * 1024 * 1024,
		DBBlockCacheCapacity:     32 * 1024 * 1024,
		DBDisableSeeksCompaction: false,
		RetrievalCaching:         true,
	}

	log("data dir: %s", dataDir)
	log("starting bee-lite node (ultra-light, mainnet)...")
	startedAt := time.Now()
	node, err := mobile.StartNode(opts, keystorePassword, "3")
	if err != nil {
		die("StartNode: %v", err)
	}
	log("node started in %s", time.Since(startedAt).Round(time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()

	if err := waitForHealth(ctx); err != nil {
		_ = node.Shutdown()
		die("/health never responded: %v", err)
	}
	log("GET /health → 200 OK  (elapsed %s)", time.Since(startedAt).Round(time.Millisecond))

	log("connected peers: %d", node.ConnectedPeerCount())

	log("shutting down...")
	if err := node.Shutdown(); err != nil {
		die("Shutdown: %v", err)
	}
	log("shutdown clean  (total elapsed %s)", time.Since(startedAt).Round(time.Millisecond))
}

func waitForHealth(ctx context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(healthPoll)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timed out after %s, last error: %v",
					healthTimeout, lastErr)
			}
			return fmt.Errorf("timed out after %s", healthTimeout)
		case <-ticker.C:
		}

		resp, err := client.Get(healthURL)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			log("  /health body: %s", string(body))
			return nil
		}
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
}

func log(format string, args ...any) {
	fmt.Printf("[hostprobe] "+format+"\n", args...)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[hostprobe] ERROR: "+format+"\n", args...)
	os.Exit(1)
}
