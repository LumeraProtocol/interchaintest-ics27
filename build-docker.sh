#!/bin/bash
# build-docker.sh - Build lumerad Docker image from GitHub releases

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${IMAGE_NAME:-lumerad-local}"
IMAGE_TAG="${IMAGE_TAG:-local}"
LUMERA_VERSION="${LUMERA_VERSION:-v1.10.1}"

echo "=================================================="
echo "Building Lumerad Docker Image"
echo "=================================================="
echo "Script dir:       $SCRIPT_DIR"
echo "Lumera version:   $LUMERA_VERSION"
echo "Image name:       $IMAGE_NAME:$IMAGE_TAG"
echo "=================================================="

# Ensure claims.csv exists (create empty if missing)
if [ ! -f "$SCRIPT_DIR/claims.csv" ]; then
    echo "Warning: claims.csv not found, creating empty file"
    touch "$SCRIPT_DIR/claims.csv"
fi

echo ""
echo "Downloading lumerad $LUMERA_VERSION and building Docker image..."
echo ""

docker build \
    --build-arg LUMERA_VERSION="$LUMERA_VERSION" \
    -t "$IMAGE_NAME:$IMAGE_TAG" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$SCRIPT_DIR"

echo ""
echo "Successfully built Docker image: $IMAGE_NAME:$IMAGE_TAG"
echo ""
docker images "$IMAGE_NAME:$IMAGE_TAG"

echo ""
echo "=================================================="
echo "To test the image:"
echo "  docker run --rm $IMAGE_NAME:$IMAGE_TAG lumerad version"
echo ""
echo "To use in tests:"
echo "  export USE_LOCAL_IMAGE=true"
echo "  go test -v ./..."
echo ""
echo "To build a different version:"
echo "  LUMERA_VERSION=v1.9.1 ./build-docker.sh"
echo "=================================================="
