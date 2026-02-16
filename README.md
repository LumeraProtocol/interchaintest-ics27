# Lumera Interchaintest Suite

End-to-end testing for Lumera blockchain using [interchaintest](https://github.com/strangelove-ventures/interchaintest).

## Overview

This test suite provides:

- **ICA (Interchain Accounts) testing** between Osmosis and Lumera
- **Genesis configuration testing** for Lumera
- **Local Docker image support** for testing unreleased changes

## Quick Start

### Prerequisites

- Docker
- Go 1.23+
- Make (optional, for convenience commands)

### Run Tests

```bash
# Run all tests (uses LUMERA_VERSION from Makefile, currently v1.10.1)
make test

# Override the version
make test LUMERA_VERSION=v1.11.0

# Genesis tests only
make test-genesis

# ICA tests only
make test-ica
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
# All tests with local image
make test-local

# Genesis only with local image
make test-genesis-local

# ICA only with local image
make test-ica-local

# Build + test in one step
make full-test
```

## Testing Genesis Only

To test genesis configuration without running full ICA tests:

```bash
# Test genesis
make test-genesis

# Test with local image
make test-genesis-local
```

This will:

1. Start a Lumera chain with modified genesis
2. Verify all genesis modifications are correct
3. Check that claims.csv is present (if available)

## Genesis Modifications

The test suite automatically modifies genesis:

- **Denoms**: `bond_denom` and `mint_denom` set to `ulume`
- **ICA host**: Enabled with all message types allowed
- **Crisis module**: Removed (not present since v1.10.x)
- **NFT module**: Removed (unsupported)
- **Consensus params**: Configured via x/consensus module

## Environment Variables

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `USE_LOCAL_IMAGE` | `false` | Use locally built Docker image |
| `LUMERA_VERSION` | `v1.10.1` | Lumera version to test (overridable in Makefile) |
| `IMAGE_NAME` | `lumerad-local` | Local Docker image name |
| `IMAGE_TAG` | `local` | Local Docker image tag |

## Project Structure

```bash
interchaintest/
├── chain_config.go          # Chain configuration
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

# Run all tests
make test
make test-local              # with local image

# Genesis tests
make test-genesis
make test-genesis-local

# ICA tests
make test-ica
make test-ica-local

# Build + test
make full-test

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
make test-genesis
```

### Claims.csv Not Found

The entrypoint script in the Docker image automatically copies `claims.csv` from `/tmp/claims.csv` to `$HOME/.lumera/config/claims.csv` when the container starts.

To verify:

```bash
# Build image
./build-docker.sh

# Test manually
docker run --rm lumerad-local:local ls -la /home/lumera/.lumera/config/
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
3. **Test genesis**: `make test-genesis-local`
4. **Run full ICA tests**: `make test-ica-local`

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
2. Add checks in `genesis_test.go`
3. Update this README with new features

## Resources

- [Interchaintest Documentation](https://github.com/strangelove-ventures/interchaintest)
- [ICS-27 (Interchain Accounts)](https://github.com/cosmos/ibc/tree/main/spec/app/ics-027-interchain-accounts)
- [Lumera Documentation](https://docs.lumera.xyz)
