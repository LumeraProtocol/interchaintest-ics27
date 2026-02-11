#!/bin/bash
# start-lumera-standalone.sh - Start Lumera standalone for manual testing
# This script starts a single Lumera node with modified genesis for testing

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${IMAGE_NAME:-lumerad-local:local}"
CONTAINER_NAME="${CONTAINER_NAME:-lumera-standalone}"
VERSION="${LUMERA_VERSION:-v1.10.1}"
# Use non-default ports to avoid conflicts with locally running nodes
PORT_P2P=36656
PORT_RPC=36657
PORT_API=11317
PORT_GRPC=19090

echo "=================================================="
echo "Starting Lumera Standalone Node"
echo "=================================================="
echo "Image:      $IMAGE_NAME"
echo "Version:    $VERSION"
echo "Container:  $CONTAINER_NAME"
echo "=================================================="
echo ""

# Clean up existing container
if docker ps -a | grep -q "$CONTAINER_NAME"; then
    echo "Removing existing container..."
    docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
fi

# Use a named Docker volume (avoids all host permission issues)
VOLUME_NAME="lumera-standalone-data"
docker volume rm "$VOLUME_NAME" 2>/dev/null || true
docker volume create "$VOLUME_NAME" >/dev/null
trap "docker volume rm $VOLUME_NAME 2>/dev/null || true" EXIT

# Build genesis modification script
GENESIS_SCRIPT='
set -e
HOME_DIR=/home/lumera/.lumera
lumerad init test-node --chain-id lumera-testnet-2 --home "$HOME_DIR" --default-denom ulume --overwrite 2>&1

# Set bond denom to ulume throughout genesis
GENESIS="$HOME_DIR/config/genesis.json"
TMP=/tmp/genesis_tmp.json
jq ".app_state.staking.params.bond_denom = \"ulume\" | .app_state.mint.params.mint_denom = \"ulume\" | .app_state.crisis.constant_fee.denom = \"ulume\" | .app_state.gov.params.min_deposit[0].denom = \"ulume\"" "$GENESIS" > "$TMP" && mv "$TMP" "$GENESIS"

# Create validator key
lumerad keys add validator --keyring-backend test --home "$HOME_DIR" 2>&1
VALIDATOR_ADDR=$(lumerad keys show validator -a --keyring-backend test --home "$HOME_DIR")

# Fund validator account and add genesis account
lumerad genesis add-genesis-account "$VALIDATOR_ADDR" 100000000000ulume --keyring-backend test --home "$HOME_DIR"

# Create gentx (validator staking tx)
lumerad genesis gentx validator 50000000000ulume --chain-id lumera-testnet-2 --keyring-backend test --home "$HOME_DIR" 2>&1

# Collect gentxs
lumerad genesis collect-gentxs --home "$HOME_DIR" 2>&1

# Modify genesis
GENESIS="$HOME_DIR/config/genesis.json"
TMP=/tmp/genesis_tmp.json

# Remove NFT module
jq "del(.app_state.nft)" "$GENESIS" > "$TMP" && mv "$TMP" "$GENESIS"

# Version-specific: remove crisis module for v1.10.x
if echo "$LUMERA_VERSION" | grep -q "^v1\.10"; then
    jq "del(.app_state.crisis)" "$GENESIS" > "$TMP" && mv "$TMP" "$GENESIS"
fi

# Copy claims.csv and update total_claimable_amount to match
if [ -f /tmp/claims.csv ] && [ -s /tmp/claims.csv ]; then
    cp /tmp/claims.csv /home/lumera/.lumera/config/claims.csv
    TOTAL=$(awk -F"," "{sum += \$2} END {print sum}" /tmp/claims.csv)
    jq ".app_state.claim.total_claimable_amount = \"$TOTAL\"" "$GENESIS" > "$TMP" && mv "$TMP" "$GENESIS"
    echo "Set total_claimable_amount to $TOTAL"
fi

# Fix ownership for lumera user
chown -R 1025:1025 "$HOME_DIR"
'

# Initialize and configure (all inside Docker as root)
echo "Initializing and configuring chain..."
docker run --rm --user root \
    -e LUMERA_VERSION="$VERSION" \
    -v "$VOLUME_NAME:/home/lumera/.lumera" \
    "$IMAGE_NAME" \
    sh -c "$GENESIS_SCRIPT"

echo "Chain initialized for $VERSION"

# Start the node
echo ""
echo "Starting Lumera node..."
echo "   RPC:  http://localhost:$PORT_RPC"
echo "   API:  http://localhost:$PORT_API"
echo "   gRPC: localhost:$PORT_GRPC"
echo ""
echo "Press Ctrl+C to stop"
echo ""

docker run --rm -it \
    --name "$CONTAINER_NAME" \
    -v "$VOLUME_NAME:/home/lumera/.lumera" \
    -p "$PORT_P2P:26656" \
    -p "$PORT_RPC:26657" \
    -p "$PORT_API:1317" \
    -p "$PORT_GRPC:9090" \
    "$IMAGE_NAME" \
    lumerad start --home /home/lumera/.lumera --minimum-gas-prices=0.025ulume
