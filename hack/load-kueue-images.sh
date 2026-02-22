#!/bin/bash
# Load Kueue images into Kind cluster

set -e

CLUSTER_NAME=${1:-kubenexus-test}
KUEUE_VERSION="v0.10.0"
RBAC_PROXY_VERSION="v0.16.0"

echo "=== Loading Kueue Images into Kind ==="
echo "Cluster: $CLUSTER_NAME"
echo "Kueue Version: $KUEUE_VERSION"
echo "RBAC Proxy Version: $RBAC_PROXY_VERSION"
echo

# Pull images locally first
echo "1. Pulling Kueue image locally..."
docker pull registry.k8s.io/kueue/kueue:${KUEUE_VERSION} || {
    echo "Failed to pull image from registry.k8s.io"
    echo "Trying alternative: gcr.io mirror..."
    docker pull gcr.io/k8s-staging-kueue/kueue:${KUEUE_VERSION} || {
        echo "ERROR: Cannot pull Kueue image from any registry"
        echo "Network/TLS issues detected."
        exit 1
    }
    # Tag it for Kind
    docker tag gcr.io/k8s-staging-kueue/kueue:${KUEUE_VERSION} registry.k8s.io/kueue/kueue:${KUEUE_VERSION}
}

echo
echo "2. Pulling kube-rbac-proxy image locally..."
docker pull registry.k8s.io/kubebuilder/kube-rbac-proxy:${RBAC_PROXY_VERSION} || {
    echo "Failed to pull RBAC proxy from registry.k8s.io"
    echo "Trying alternative: gcr.io mirror..."
    docker pull gcr.io/kubebuilder/kube-rbac-proxy:${RBAC_PROXY_VERSION} && \
    docker tag gcr.io/kubebuilder/kube-rbac-proxy:${RBAC_PROXY_VERSION} registry.k8s.io/kubebuilder/kube-rbac-proxy:${RBAC_PROXY_VERSION} || {
        echo "ERROR: Cannot pull kube-rbac-proxy image"
        exit 1
    }
}

echo
echo "3. Loading images into Kind cluster..."
kind load docker-image registry.k8s.io/kueue/kueue:${KUEUE_VERSION} --name ${CLUSTER_NAME}
kind load docker-image registry.k8s.io/kubebuilder/kube-rbac-proxy:${RBAC_PROXY_VERSION} --name ${CLUSTER_NAME}

echo
echo "4. Verifying images in Kind nodes..."
docker exec ${CLUSTER_NAME}-control-plane crictl images | grep -E "kueue|rbac-proxy" || echo "Image verification skipped"

echo
echo "=== Images Loaded Successfully ==="
echo
echo "Images loaded:"
echo "  - registry.k8s.io/kueue/kueue:${KUEUE_VERSION}"
echo "  - registry.k8s.io/kubebuilder/kube-rbac-proxy:${RBAC_PROXY_VERSION}"
echo
echo "Now you can install Kueue:"
echo "  kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"
