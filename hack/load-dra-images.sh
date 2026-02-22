#!/usr/bin/env bash

# Load DRA Example Driver images into Kind cluster
# This bypasses TLS certificate issues with registry.k8s.io

set -e

CLUSTER_NAME="${1:-kubenexus-test}"
DRA_VERSION="v0.2.1"

echo "=========================================="
echo "Loading DRA Driver Images into Kind"
echo "=========================================="
echo "Cluster: $CLUSTER_NAME"
echo "Version: $DRA_VERSION"
echo ""

# Check if cluster exists
if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
  echo "Error: Kind cluster '$CLUSTER_NAME' not found"
  exit 1
fi

# List of images to load
IMAGES=(
  "registry.k8s.io/dra-example-driver/dra-example-driver:${DRA_VERSION}"
)

echo "Images to load:"
for img in "${IMAGES[@]}"; do
  echo "  - $img"
done
echo ""

# Pull and load each image
for img in "${IMAGES[@]}"; do
  echo "Processing: $img"
  
  # Check if image already exists locally
  if docker images --format "{{.Repository}}:{{.Tag}}" | grep -q "^${img}$"; then
    echo "  ✓ Image already exists locally"
  else
    echo "  Pulling from registry..."
    if docker pull "$img"; then
      echo "  ✓ Pulled successfully"
    else
      echo "  ✗ Failed to pull $img"
      echo "  This is likely due to TLS certificate issues or network problems"
      exit 1
    fi
  fi
  
  # Load into Kind cluster
  echo "  Loading into Kind cluster..."
  if kind load docker-image "$img" --name "$CLUSTER_NAME"; then
    echo "  ✓ Loaded into cluster"
  else
    echo "  ✗ Failed to load into cluster"
    exit 1
  fi
  echo ""
done

echo "=========================================="
echo "All images loaded successfully!"
echo "=========================================="
echo ""
echo "Verify with:"
echo "  docker exec -it ${CLUSTER_NAME}-control-plane crictl images | grep dra"
echo ""
