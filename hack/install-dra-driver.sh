#!/usr/bin/env bash

# Install the DRA Example Driver to mock GPU devices for testing
# This allows testing DRA workflows without physical GPUs

set -e

CLUSTER_NAME="${1:-kubenexus-test}"
DRA_DRIVER_VERSION="v0.2.1"
DRA_DRIVER_REPO="https://github.com/kubernetes-sigs/dra-example-driver.git"
DRA_DRIVER_DIR="/tmp/dra-example-driver"

echo "=========================================="
echo "Installing DRA Example Driver"
echo "=========================================="
echo "Cluster: $CLUSTER_NAME"
echo "Version: $DRA_DRIVER_VERSION"
echo ""

# Check prerequisites
echo "Checking prerequisites..."
for cmd in kubectl kind docker helm; do
  if ! command -v $cmd &> /dev/null; then
    echo "Error: $cmd is required but not installed"
    exit 1
  fi
done

# Check if cluster exists
if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
  echo "Error: Kind cluster '$CLUSTER_NAME' not found"
  echo "Available clusters:"
  kind get clusters
  exit 1
fi

echo "✓ All prerequisites met"
echo ""

# Clone DRA driver repository
echo "Cloning DRA Example Driver repository..."
if [ -d "$DRA_DRIVER_DIR" ]; then
  echo "Removing existing directory..."
  rm -rf "$DRA_DRIVER_DIR"
fi

git clone --depth 1 --branch "$DRA_DRIVER_VERSION" "$DRA_DRIVER_REPO" "$DRA_DRIVER_DIR"
cd "$DRA_DRIVER_DIR"

echo "✓ Repository cloned"
echo ""

# Use the image preload script (handles TLS certificate issues)
echo "Loading DRA driver images into cluster..."
cd "$DRA_DRIVER_DIR/.."
if [ -f "${OLDPWD}/hack/load-dra-images.sh" ]; then
  "${OLDPWD}/hack/load-dra-images.sh" "$CLUSTER_NAME"
else
  echo "Warning: load-dra-images.sh not found, trying direct pull..."
  IMAGE_NAME="registry.k8s.io/dra-example-driver/dra-example-driver:${DRA_DRIVER_VERSION}"
  docker pull "$IMAGE_NAME" || {
    echo "Error: Failed to pull DRA driver image"
    echo "TLS certificate issues detected. Please check your network settings."
    exit 1
  }
  kind load docker-image "$IMAGE_NAME" --name "$CLUSTER_NAME"
fi

cd "$DRA_DRIVER_DIR"
echo "✓ Images loaded into cluster"
echo ""

# Install cert-manager (required for validation webhook, optional but recommended)
echo "Installing cert-manager..."
if kubectl get namespace cert-manager &> /dev/null; then
  echo "cert-manager namespace exists, checking pods..."
  
  # Check if cert-manager pods are having image pull issues
  if kubectl get pods -n cert-manager 2>/dev/null | grep -q "ImagePullBackOff\|ErrImagePull"; then
    echo "⚠ Detected ImagePullBackOff issues, cleaning up..."
    helm uninstall cert-manager -n cert-manager || true
    kubectl delete namespace cert-manager --wait || true
    sleep 5
  else
    echo "cert-manager already installed and healthy, skipping..."
    kubectl get pods -n cert-manager
    echo ""
    # Skip to DRA installation
    cd "$DRA_DRIVER_DIR"
  fi
fi

if ! kubectl get namespace cert-manager &> /dev/null; then
  # Preload cert-manager images first
  echo "Loading cert-manager images into cluster..."
  if [ -f "${OLDPWD}/hack/load-cert-manager-images.sh" ]; then
    "${OLDPWD}/hack/load-cert-manager-images.sh" "$CLUSTER_NAME"
  else
    echo "Warning: load-cert-manager-images.sh not found"
  fi
  
  helm install \
    --repo https://charts.jetstack.io \
    --version v1.16.3 \
    --create-namespace \
    --namespace cert-manager \
    --wait \
    --set crds.enabled=true \
    --set global.imagePullPolicy=IfNotPresent \
    cert-manager \
    cert-manager

  echo "✓ cert-manager installed"
  
  # Wait for cert-manager webhook to be ready
  echo "Waiting for cert-manager webhook..."
  kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=webhook -n cert-manager --timeout=120s || true
  sleep 10
fi
echo ""

# Install DRA driver via Helm
echo "Installing DRA Example Driver..."
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  --set webhook.enabled=true \
  --set image.pullPolicy=IfNotPresent \
  dra-example-driver \
  deployments/helm/dra-example-driver

echo "✓ DRA driver installed"
echo ""

# Wait for driver pods to be ready
echo "Waiting for driver pods to be ready..."
kubectl wait --for=condition=Ready pod \
  -l app.kubernetes.io/name=dra-example-driver \
  -n dra-example-driver \
  --timeout=120s

echo "✓ Driver pods ready"
echo ""

# Verify installation
echo "Verifying installation..."
echo ""
echo "Driver pods:"
kubectl get pods -n dra-example-driver
echo ""

echo "ResourceSlices (mock GPUs):"
kubectl get resourceslice
echo ""

# Show summary of mock GPUs
echo "=========================================="
echo "Installation Complete!"
echo "=========================================="
echo ""
echo "Mock GPU devices created. To see details:"
echo "  kubectl get resourceslice -o yaml"
echo ""
echo "To test DRA with pods:"
echo "  kubectl apply -f test/e2e/fixtures/dra-gpu-test.yaml"
echo ""
echo "Cleanup:"
echo "  cd $DRA_DRIVER_DIR && ./demo/delete-cluster.sh"
echo "  Or: helm uninstall dra-example-driver -n dra-example-driver"
echo ""
