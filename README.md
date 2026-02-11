# Lumera Interchaintest Suite

End-to-end testing for Lumera blockchain using [interchaintest](https://github.com/strangelove-ventures/interchaintest).

## Overview

This test suite provides:

- **ICA (Interchain Accounts) testing** between Osmosis and Lumera
- **Genesis configuration testing** for multiple Lumera versions
- **Local Docker image support** for testing unreleased changes
- **Version-specific genesis modifications** (v1.9.1 vs v1.10.1)

## Quick Start

### Prerequisites

- Docker
- Go 1.23+
- Make (optional, for convenience commands)

### Run Tests

```bash
# Run all tests for v1.10.1 (default)
make test

# Run all tests for v1.9.1
make test-v1.9.1

# Genesis tests only
make test-genesis-v1.10.1
make test-genesis-v1.9.1

# ICA tests only
make test-ica-v1.10.1
make test-ica-v1.9.1

# Test both versions
make test-all-versions
```

## Using Local Docker Images

### 1. Build Local Lumerad Image

```bash
# From the interchaintest directory
./build-docker.sh

# Or use make
make build-docker
```

This builds `lumerad-local:latest` from your local lumera source code.

**Important**: The build script will:

- Build lumerad from `../lumera/` directory
- Include `claims.csv` in the image
- Set up an entrypoint that copies `claims.csv` to `.lumera/config/` automatically

### 2. Run Tests with Local Image

```bash
# All tests with local image (v1.10.1)
make test-local

# Specific version with local image
make test-v1.9.1-local
make test-v1.10.1-local

# Genesis only with local image
make test-genesis-v1.9.1-local
make test-genesis-v1.10.1-local

# ICA only with local image
make test-ica-v1.9.1-local
make test-ica-v1.10.1-local
```

## Testing Genesis Only

To test genesis configuration without running full ICA tests:

```bash
# Test v1.10.1 genesis (default)
make test-genesis

# Test v1.9.1 genesis
make test-genesis-v1.9.1

# Test with local image
make test-genesis-v1.10.1-local
make test-genesis-v1.9.1-local
```

This will:

1. Start a Lumera chain with modified genesis
2. Verify all genesis modifications are correct
3. Check that claims.csv is present (if available)
4. Run version-specific validation

## Lumera Versions

### v1.9.1 and Earlier

- **Includes**: crisis module
- **Consensus params**: Stored in legacy x/params module
- **Modules**: action, supernode, ICA, PFM (no NFT)

### v1.10.0 and v1.10.1

- **Removed**: crisis module
- **Consensus params**: Migrated to x/consensus module
- **Modules**: action, supernode, ICA, PFM (no NFT)

## Genesis Modifications

The test suite automatically modifies genesis for both versions:

### Common Modifications (All Versions)

**Action Module:**

```json
{
  "base_action_fee": {"denom": "ulume", "amount": "10000"},
  "max_actions_per_block": "10",
  "min_super_nodes": "3",
  "super_node_fee_share": "1.0"
}
```

**Supernode Module:**

```json
{
  "min_cpu_cores": "8",
  "min_mem_gb": "16",
  "min_storage_gb": "1000",
  "metrics_update_interval_blocks": "400"
}
```

**ICA Host:**

- Enabled with allowlisted messages:
  - `/lumera.action.v1.MsgRequestAction`
  - `/lumera.action.v1.MsgApproveAction`
  - Standard Cosmos messages (bank, staking, distribution)

**Modules:**

- ✅ Packet Forward Middleware (PFM)
- ❌ NFT module (removed)

### Version-Specific Modifications

**v1.9.1:**

- ✅ Crisis module present

**v1.10.1:**

- ❌ Crisis module removed
- ✅ Consensus params in x/consensus

## Environment Variables

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `USE_LOCAL_IMAGE` | `false` | Use locally built Docker image |
| `LUMERA_VERSION` | `v1.10.1` | Lumera version to test |
| `IMAGE_NAME` | `lumerad-local` | Local Docker image name |
| `IMAGE_TAG` | `latest` | Local Docker image tag |

## Project Structure

```bash
interchaintest/
├── chain_config.go          # Unified chain configuration
├── ica_test.go              # ICA e2e tests
├── genesis_test.go          # Genesis verification tests
├── Dockerfile               # Lumerad Docker image
├── build-docker.sh          # Build script
├── Makefile                 # Convenience commands
└── README.md                # This file
```

## Makefile Commands

```bash
# Show all available targets
make help

# Build local Docker image
make build-docker

# Run all tests (v1.10.1 by default)
make test
make test-local              # with local image

# Version-specific tests (no env vars needed)
make test-v1.9.1             # all tests for v1.9.1
make test-v1.10.1            # all tests for v1.10.1
make test-v1.9.1-local       # with local image
make test-v1.10.1-local      # with local image

# Genesis tests
make test-genesis-v1.9.1
make test-genesis-v1.10.1
make test-genesis-v1.9.1-local
make test-genesis-v1.10.1-local

# ICA tests
make test-ica-v1.9.1
make test-ica-v1.10.1
make test-ica-v1.9.1-local
make test-ica-v1.10.1-local

# Multi-version
make test-all-versions       # test both versions
make full-test               # build + test all versions locally

# Cleanup
make clean-docker
```

## Troubleshooting

### Docker Build Fails

```bash
# Ensure lumera source is in the correct location
ls -la ../lumera/

# Check if claims.csv exists
ls -la ../lumera/claims.csv

# Rebuild from scratch
make clean-docker build-docker
```

### Genesis Modification Errors

```bash
# Test genesis modifications without full ICA flow
make test-genesis-v1.10.1

# Check specific version
make test-genesis-v1.9.1
```

### Claims.csv Not Found

The entrypoint script in the Docker image automatically copies `claims.csv` from `/tmp/claims.csv` to `$HOME/.lumera/config/claims.csv` when the container starts.

To verify:

```bash
# Build image
./build-docker.sh

# Test manually
docker run --rm lumerad-local:latest ls -la /home/lumera/.lumera/config/
```

### ICA Tests Failing

```bash
# Ensure both chains start correctly
make test-genesis

# Check logs for specific errors
make test-ica 2>&1 | tee test.log
```

## Development Workflow

1. **Make changes to lumera source code**
2. **Rebuild Docker image**: `make build-docker`
3. **Test genesis**: `make test-genesis-v1.10.1-local`
4. **Run full ICA tests**: `make test-ica-v1.10.1-local`

## CI/CD Integration

```yaml
# Example GitHub Actions workflow
- name: Build Lumerad Docker
  run: |
    cd interchaintest
    ./build-docker.sh

- name: Run Genesis Tests
  run: |
    cd interchaintest
    USE_LOCAL_IMAGE=true go test -v -run TestLumeraGenesisSetup

- name: Run ICA Tests
  run: |
    cd interchaintest
    USE_LOCAL_IMAGE=true go test -v -run TestOsmosisLumeraICA
```

## Contributing

When adding new tests:

1. Update genesis modifications in `chain_config.go` if needed
2. Add version-specific checks in `genesis_test.go`
3. Update this README with new features
4. Ensure tests pass for both v1.9.1 and v1.10.1

## Resources

- [Interchaintest Documentation](https://github.com/strangelove-ventures/interchaintest)
- [ICS-27 (Interchain Accounts)](https://github.com/cosmos/ibc/tree/main/spec/app/ics-027-interchain-accounts)
- [Lumera Documentation](https://docs.lumera.xyz)
