.PHONY: help build-docker clean-docker docker-info verify
.PHONY: test test-local test-v1.9.1 test-v1.10.1 test-v1.9.1-local test-v1.10.1-local
.PHONY: test-genesis test-genesis-local test-genesis-v1.9.1 test-genesis-v1.10.1 test-genesis-v1.9.1-local test-genesis-v1.10.1-local
.PHONY: test-ica test-ica-local test-ica-v1.9.1 test-ica-v1.10.1 test-ica-v1.9.1-local test-ica-v1.10.1-local
.PHONY: test-all-versions test-all-versions-local full-test

# Default target
help:
	@echo "Lumera Interchaintest Makefile"
	@echo ""
	@echo "Docker:"
	@echo "  build-docker              Build lumerad Docker image from local source"
	@echo "  clean-docker              Remove local Docker images"
	@echo "  docker-info               Show Docker image info"
	@echo "  verify                    Verify local setup"
	@echo ""
	@echo "Genesis tests:"
	@echo "  test-genesis-v1.9.1       Test genesis for v1.9.1"
	@echo "  test-genesis-v1.10.1      Test genesis for v1.10.1"
	@echo "  test-genesis-v1.9.1-local   ... with local image"
	@echo "  test-genesis-v1.10.1-local  ... with local image"
	@echo ""
	@echo "ICA tests:"
	@echo "  test-ica-v1.9.1           Run ICA tests for v1.9.1"
	@echo "  test-ica-v1.10.1          Run ICA tests for v1.10.1"
	@echo "  test-ica-v1.9.1-local       ... with local image"
	@echo "  test-ica-v1.10.1-local      ... with local image"
	@echo ""
	@echo "All tests for a version:"
	@echo "  test-v1.9.1               Run all tests for v1.9.1"
	@echo "  test-v1.10.1              Run all tests for v1.10.1"
	@echo "  test-v1.9.1-local           ... with local image"
	@echo "  test-v1.10.1-local          ... with local image"
	@echo ""
	@echo "Shortcuts (default v1.10.1):"
	@echo "  test-genesis              Test genesis (v1.10.1)"
	@echo "  test-genesis-local        Test genesis with local image"
	@echo "  test-ica                  Run ICA tests (v1.10.1)"
	@echo "  test-ica-local            Run ICA tests with local image"
	@echo "  test                      Run all tests (v1.10.1)"
	@echo "  test-local                Run all tests with local image"
	@echo ""
	@echo "Multi-version:"
	@echo "  test-all-versions         Test both v1.9.1 and v1.10.1"
	@echo "  test-all-versions-local   Test both versions with local image"
	@echo "  full-test                 Build + test all versions locally"

# ── Docker ──────────────────────────────────────────────

build-docker:
	@echo "Building lumerad Docker image..."
	./build-docker.sh

clean-docker:
	-docker rmi lumerad-local:local 2>/dev/null || true
	@echo "Cleanup complete"

docker-info:
	@docker images | grep -E "(REPOSITORY|lumerad-local|lumeraprotocol)" || echo "No lumera images found"

verify:
	@echo "Verifying setup..."
	@which docker > /dev/null && docker --version || echo "  Docker not found"
	@which go > /dev/null && go version || echo "  Go not found"
	@test -d ../lumera && echo "  Lumera source: found at ../lumera" || echo "  Lumera source: not found at ../lumera"
	@test -f ../lumera/claims.csv && echo "  claims.csv: found" || echo "  claims.csv: not found (optional)"

# ── Genesis tests ───────────────────────────────────────

test-genesis-v1.9.1:
	LUMERA_VERSION=v1.9.1 go test -v -timeout 10m -run TestLumeraGenesisSetup

test-genesis-v1.10.1:
	LUMERA_VERSION=v1.10.1 go test -v -timeout 10m -run TestLumeraGenesisSetup

test-genesis-v1.9.1-local: build-docker
	LUMERA_VERSION=v1.9.1 USE_LOCAL_IMAGE=true go test -v -timeout 10m -run TestLumeraGenesisSetup

test-genesis-v1.10.1-local: build-docker
	LUMERA_VERSION=v1.10.1 USE_LOCAL_IMAGE=true go test -v -timeout 10m -run TestLumeraGenesisSetup

# Shortcuts (default v1.10.1)
test-genesis: test-genesis-v1.10.1
test-genesis-local: test-genesis-v1.10.1-local

# ── ICA tests ───────────────────────────────────────────

test-ica-v1.9.1:
	LUMERA_VERSION=v1.9.1 go test -v -timeout 20m -run TestOsmosisLumeraICA

test-ica-v1.10.1:
	LUMERA_VERSION=v1.10.1 go test -v -timeout 20m -run TestOsmosisLumeraICA

test-ica-v1.9.1-local: build-docker
	LUMERA_VERSION=v1.9.1 USE_LOCAL_IMAGE=true go test -v -timeout 20m -run TestOsmosisLumeraICA

test-ica-v1.10.1-local: build-docker
	LUMERA_VERSION=v1.10.1 USE_LOCAL_IMAGE=true go test -v -timeout 20m -run TestOsmosisLumeraICA

# Shortcuts (default v1.10.1)
test-ica: test-ica-v1.10.1
test-ica-local: test-ica-v1.10.1-local

# ── All tests for a version ─────────────────────────────

test-v1.9.1:
	LUMERA_VERSION=v1.9.1 go test -v -timeout 30m ./...

test-v1.10.1:
	LUMERA_VERSION=v1.10.1 go test -v -timeout 30m ./...

test-v1.9.1-local: build-docker
	LUMERA_VERSION=v1.9.1 USE_LOCAL_IMAGE=true go test -v -timeout 30m ./...

test-v1.10.1-local: build-docker
	LUMERA_VERSION=v1.10.1 USE_LOCAL_IMAGE=true go test -v -timeout 30m ./...

# Shortcuts (default v1.10.1)
test: test-v1.10.1
test-local: test-v1.10.1-local

# ── Multi-version ───────────────────────────────────────

test-all-versions:
	go test -v -timeout 30m -run TestBothVersions

test-all-versions-local: build-docker
	USE_LOCAL_IMAGE=true go test -v -timeout 30m -run TestBothVersions

full-test: build-docker test-all-versions-local
