#!/bin/bash
set -e

echo "========================================="
echo "KubeNexus Scheduler Testing Setup"
echo "========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}"
command -v kind >/dev/null 2>&1 || { echo -e "${RED}kind is not installed. Install from https://kind.sigs.k8s.io/${NC}"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo -e "${RED}kubectl is not installed${NC}"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo -e "${RED}docker is not installed${NC}"; exit 1; }

echo -e "${GREEN}✓ All prerequisites found${NC}"

# Create Kind cluster
echo -e "\n${YELLOW}Creating Kind cluster (Kubernetes 1.35 with DRA enabled)...${NC}"
if kind get clusters | grep -q kubenexus-test; then
    echo "Cluster 'kubenexus-test' already exists. Deleting..."
    kind delete cluster --name kubenexus-test
fi

kind create cluster --config hack/kind-cluster-v1.35.yaml --name kubenexus-test --wait 5m
echo -e "${GREEN}✓ Kind cluster created${NC}"

# Set kubectl context
kubectl config use-context kind-kubenexus-test

# Build Docker image
echo -e "\n${YELLOW}Building scheduler image...${NC}"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags '-w' -o bin/kubenexus-scheduler-linux cmd/main.go
docker build -t kubenexus-scheduler:latest .
echo -e "${GREEN}✓ Image built${NC}"

# Load image into Kind
echo -e "\n${YELLOW}Loading image into Kind cluster...${NC}"
kind load docker-image kubenexus-scheduler:latest --name kubenexus-test
echo -e "${GREEN}✓ Image loaded${NC}"

# Deploy CRDs
echo -e "\n${YELLOW}Deploying CRDs...${NC}"
kubectl apply -f config/crd-resourcereservation.yaml
echo -e "${GREEN}✓ CRDs deployed${NC}"

# Deploy scheduler
echo -e "\n${YELLOW}Deploying KubeNexus scheduler...${NC}"
kubectl apply -f deploy/kubenexus-scheduler.yaml
kubectl wait --for=condition=available --timeout=120s deployment/kubenexus-scheduler -n kube-system
echo -e "${GREEN}✓ Scheduler deployed and ready${NC}"

# Verify deployment
echo -e "\n${YELLOW}Verifying deployment...${NC}"
kubectl get pods -n kube-system -l app=kubenexus-scheduler
kubectl logs -n kube-system -l app=kubenexus-scheduler --tail=20

echo -e "\n${GREEN}========================================="
echo "Setup complete!"
echo "=========================================${NC}"
echo ""
echo "Next steps:"
echo "  1. Run test workloads: ./hack/test-workloads.sh"
echo "  2. Check scheduler logs: kubectl logs -n kube-system -l app=kubenexus-scheduler -f"
echo "  3. Cleanup: kind delete cluster --name kubenexus-test"
