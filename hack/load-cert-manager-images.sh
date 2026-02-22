#!/usr/bin/env bash

# Load cert-manager images into Kind cluster
# This bypasses TLS certificate issues with registry.k8s.io

set -e

CLUSTER_NAME="${1:-kubenexus-test}"
CERT_MANAGER_VERSION="v1.16.3"

echo "=========================================="
echo "Loading cert-manager Images into Kind"
echo "=========================================="
echo "Cluster: $CLUSTER_NAME"
echo "Version: $CERT_MANAGER_VERSION"
echo ""

# Check if cluster exists
if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
  echo "Error: Kind cluster '$CLUSTER_NAME' not found"
  exit 1
fi

# List of cert-manager images
IMAGES=(
  "quay.io/jetstack/cert-manager-controller:${CERT_MANAGER_VERSION}"
  "quay.io/jetstack/cert-manager-webhook:${CERT_MANAGER_VERSION}"
  "quay.io/jetstack/cert-manager-cainjector:${CERT_MANAGER_VERSION}"
  "quay.io/jetstack/cert-manager-acmesolver:${CERT_MANAGER_VERSION}"
  "quay.io/jetstack/cert-manager-ctl:${CERT_MANAGER_VERSION}"
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
      echo "  ⚠ Failed to pull $img"
      echo "  Continuing with next image..."
      continue
    fi
  fi
  
  # Load into Kind cluster
  echo "  Loading into Kind cluster..."
  if kind load docker-image "$img" --name "$CLUSTER_NAME"; then
    echo "  ✓ Loaded into cluster"
  else
    echo "  ⚠ Failed to load into cluster"
  fi
  echo ""
done

echo "=========================================="
echo "Image loading complete!"
echo "=========================================="
echo ""
echo "Verify with:"
echo "  docker exec -it ${CLUSTER_NAME}-control-plane crictl images | grep cert-manager"
echo ""
