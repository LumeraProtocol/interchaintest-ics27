.PHONY: help build-docker clean-docker docker-info verify
.PHONY: test test-local test-genesis test-genesis-local test-ica test-ica-local full-test

# Lumera version — override via: make test LUMERA_VERSION=v1.10.1
LUMERA_VERSION ?= v1.10.1

# Default target
help:
	@echo "Lumera Interchaintest Makefile"
	@echo ""
	@echo "  LUMERA_VERSION=$(LUMERA_VERSION)  (override with LUMERA_VERSION=vX.Y.Z)"
	@echo ""
	@echo "Docker:"
	@echo "  build-docker              Build lumerad Docker image"
	@echo "  clean-docker              Remove local Docker images"
	@echo "  docker-info               Show Docker image info"
	@echo "  verify                    Verify local setup"
	@echo ""
	@echo "Tests:"
	@echo "  test-genesis              Test genesis configuration"
	@echo "  test-genesis-local        Test genesis with local image"
	@echo "  test-ica                  Run ICA tests"
	@echo "  test-ica-local            Run ICA tests with local image"
	@echo "  test                      Run all tests"
	@echo "  test-local                Run all tests with local image"
	@echo "  full-test                 Build + run all tests locally"

# ── Docker ──────────────────────────────────────────────

build-docker:
	@echo "Building lumerad Docker image..."
	LUMERA_VERSION=$(LUMERA_VERSION) ./build-docker.sh

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

test-genesis:
	LUMERA_VERSION=$(LUMERA_VERSION) go test -v -timeout 10m -run TestLumeraGenesisSetup

test-genesis-local: build-docker
	LUMERA_VERSION=$(LUMERA_VERSION) USE_LOCAL_IMAGE=true go test -v -timeout 10m -run TestLumeraGenesisSetup

# ── ICA tests ───────────────────────────────────────────

test-ica:
	LUMERA_VERSION=$(LUMERA_VERSION) go test -v -timeout 20m -run TestOsmosisLumeraICA

test-ica-local: build-docker
	LUMERA_VERSION=$(LUMERA_VERSION) USE_LOCAL_IMAGE=true go test -v -timeout 20m -run TestOsmosisLumeraICA

# ── All tests ───────────────────────────────────────────

test:
	LUMERA_VERSION=$(LUMERA_VERSION) go test -v -timeout 30m ./...

test-local: build-docker
	LUMERA_VERSION=$(LUMERA_VERSION) USE_LOCAL_IMAGE=true go test -v -timeout 30m ./...

full-test: test-local
