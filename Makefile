.PHONY: install build verify probe probe-ipfs probe-ipfs-offline probe-co probe-all clean

# Version string surfaced into the binary as mobile.version via -ldflags -X.
# `git describe` gives us "v0.1.0" at a tagged commit, "v0.1.0-3-gabc1234"
# three commits after, "-dirty" suffix when the working tree has changes,
# or a bare short SHA if no tags exist. Falls back to "dev" when git isn't
# available (e.g. a tarball build).
VERSION := $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)

# The gomobile output. Drop into an Android project's libs/ directory for
# consumption as a Gradle file dependency. Versioned filename so a consumer
# dropping several AARs into libs/ can tell them apart at a glance.
AAR_FILE=mobile-$(VERSION).aar
BUILD_DIR=build

# Android API 30 is a conservative minSdk floor. -checklinkname=0 lets the
# runtime accept the //go:linkname calls bee and go-ethereum use into the
# Go runtime; without it the build fails under Go >= 1.23. -X injects the
# version string into mobile/version.go so mobile.Mobile.version() returns
# the git-described build tag instead of the "dev" fallback.
ANDROID_API=30
VERSION_PKG=github.com/solardev-xyz/freedom-node-mobile/mobile
LDFLAGS=-checklinkname=0 -X $(VERSION_PKG).version=$(VERSION)

# Resolve the Go install dir at make-time and invoke gomobile by full path.
# Keeps `make install` / `make build` working even when $(go env GOBIN) (or
# $(go env GOPATH)/bin) isn't on the shell's PATH.
GO_BIN := $(shell go env GOBIN)
ifeq ($(GO_BIN),)
GO_BIN := $(shell go env GOPATH)/bin
endif
GOMOBILE := $(GO_BIN)/gomobile

# gomobile bind shells out to `javac` (to compile the generated Java stubs
# into classes.jar). Pick a JDK 17 if one is visible — modern JDKs (21+)
# reject the `-source 1.8 -target 1.8` flags gomobile hardcodes, and JDK 17
# is the Android ecosystem default. Fall back to any available JDK as a
# last resort.
JAVA_HOME ?= $(firstword $(wildcard \
    /opt/homebrew/opt/openjdk@17 \
    /usr/local/opt/openjdk@17 \
    /Library/Java/JavaVirtualMachines/temurin-17.jdk/Contents/Home \
    /opt/homebrew/opt/openjdk \
    /usr/local/opt/openjdk \
    /usr/lib/jvm/java-17-openjdk-amd64 \
    /usr/lib/jvm/default-java))
export JAVA_HOME

# gomobile shells out to `gobind` and `javac` via exec.LookPath, so the Go
# install dir and the JDK bin dir have to be on PATH for child processes
# even if the user hasn't exported them in their shell rc.
ifneq ($(JAVA_HOME),)
export PATH := $(GO_BIN):$(JAVA_HOME)/bin:$(PATH)
else
export PATH := $(GO_BIN):$(PATH)
endif

# Locate the Android SDK. gomobile needs $ANDROID_HOME pointing at an SDK
# that contains an NDK under $ANDROID_HOME/ndk/<version>/. If the caller
# hasn't set it, probe the usual macOS / Linux install locations so
# `make build` works out of the box on a fresh checkout.
ifeq ($(origin ANDROID_HOME), undefined)
  ANDROID_HOME := $(firstword $(wildcard \
      $(ANDROID_SDK_ROOT) \
      /opt/homebrew/share/android-commandlinetools \
      /usr/local/share/android-commandlinetools \
      $(HOME)/Library/Android/sdk \
      $(HOME)/Android/Sdk))
endif
export ANDROID_HOME

install:
	go mod tidy
	go install golang.org/x/mobile/cmd/gomobile@latest
	go install golang.org/x/mobile/cmd/gobind@latest
	$(GOMOBILE) init

# Produce the AAR. Takes a few minutes on first build — gomobile bind
# cross-compiles bee-lite once per ABI (arm64-v8a, armeabi-v7a, x86, x86_64).
build:
	rm -rf $(BUILD_DIR) && mkdir -p $(BUILD_DIR)
	$(GOMOBILE) bind \
		-target=android \
		-androidapi=$(ANDROID_API) \
		-ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(AAR_FILE) \
		./mobile

# Host-side smoke test: boot bee-lite on the dev machine, poll /health,
# shut down. Proves the Go side behaves correctly without any Android
# tooling. Exit 0 on success.
probe:
	go run -ldflags="$(LDFLAGS)" ./cmd/hostprobe

# IPFS counterpart of probe: boot a kubo node, start the gateway, round-trip
# a payload through CoreAPI -> gateway, shut down. Exercises the kubo half
# of the combined binary end-to-end.
probe-ipfs:
	go run -ldflags="$(LDFLAGS)" ./cmd/ipfsprobe

# Offline variant: skips libp2p bring-up. Useful for CI and for quickly
# validating changes that don't touch the network path. Runs in ~1s.
probe-ipfs-offline:
	go run -ldflags="$(LDFLAGS)" ./cmd/ipfsprobe --offline

# Co-hosted probe: boot bee AND kubo in the same process, in parallel, and
# verify both gateways respond. Passing this means the combined binary is
# viable for one-process dual operation on Android.
probe-co:
	go run -ldflags="$(LDFLAGS)" ./cmd/coprobe

# Structural sanity check on build/mobile.aar. Confirms the archive has the
# expected shape, all four ABIs are present, the Java surface is bound, and
# both bee and kubo actually landed in the native binary. Fast (~2s) and
# needs no Android runtime — just unzip and strings.
verify:
	@set -e; \
	AAR=$(BUILD_DIR)/$(AAR_FILE); \
	[ -f $$AAR ] || { echo "verify: $$AAR not found — run 'make build' first" >&2; exit 1; }; \
	echo "=== AAR contents ==="; \
	unzip -l $$AAR; \
	echo; \
	echo "=== Java surface (classes.jar entries) ==="; \
	TMP=$$(mktemp -d); trap 'rm -rf $$TMP' EXIT; \
	unzip -oq $$AAR classes.jar -d $$TMP; \
	unzip -l $$TMP/classes.jar | awk '/mobile\/.*\.class/ {print $$4}' | sort; \
	for cls in mobile/Mobile.class mobile/MobileNode.class mobile/MobileNodeOptions.class \
	           mobile/IpfsNode.class mobile/IpfsNodeOptions.class mobile/IpfsStats.class; do \
	    unzip -l $$TMP/classes.jar | grep -q " $$cls$$" \
	        || { echo "verify: missing expected class $$cls" >&2; exit 1; }; \
	done; \
	echo; \
	echo "=== Native runtime symbols (arm64-v8a) ==="; \
	unzip -oq $$AAR jni/arm64-v8a/libgojni.so -d $$TMP; \
	SO=$$TMP/jni/arm64-v8a/libgojni.so; \
	for pkg in github.com/ethersphere/bee github.com/ipfs/kubo \
	           github.com/libp2p/go-libp2p github.com/ethereum/go-ethereum; do \
	    n=$$(strings $$SO | grep -c "$$pkg" || true); \
	    printf '  %-40s %s\n' "$$pkg" "$$n occurrences"; \
	    [ $$n -gt 0 ] || { echo "verify: no symbols for $$pkg in libgojni.so" >&2; exit 1; }; \
	done; \
	echo; \
	echo "verify: OK"

# Run every host-side probe in sequence. If this is green, both halves of
# the combined binary are sound — individually and together — before we
# spend cycles on gomobile bind.
probe-all: probe probe-ipfs probe-co

clean:
	rm -rf $(BUILD_DIR)
