# GPU Cluster Migration Guide

This guide covers testing kubenexus-scheduler with real GPU workloads on GKE and EKS.

## Table of Contents

- [Local Testing with Mock GPUs](#local-testing-with-mock-gpus)
- [GKE GPU Cluster Setup](#gke-gpu-cluster-setup)
- [EKS GPU Cluster Setup](#eks-gpu-cluster-setup)
- [Free Tier Considerations](#free-tier-considerations)
- [Testing Workflow](#testing-workflow)

## Local Testing with Mock GPUs

Before moving to cloud GPU clusters, test the full DRA workflow locally:

### Prerequisites

- Kubernetes 1.35+ Kind cluster with DRA enabled
- kubenexus-scheduler deployed
- Docker and Helm installed

### Install DRA Example Driver

The DRA Example Driver simulates GPU devices without requiring physical hardware:

```bash
# Install DRA driver with mock GPUs (8 GPUs, 80Gi each)
./hack/install-dra-driver.sh kubenexus-test

# Verify installation
kubectl get resourceslice
kubectl get pods -n dra-example-driver
```

### Run DRA Tests

```bash
# Test single GPU, dual GPU, and gang scheduling with GPUs
./test/e2e/scripts/run-dra-gpu-test.sh

# View GPU allocations
kubectl logs single-gpu-pod -n dra-gpu-test
kubectl logs dual-gpu-pod -n dra-gpu-test
```

### What Gets Tested

1. **Single GPU Allocation**: Pod requests 1 GPU via ResourceClaim
2. **Multi-GPU Allocation**: Pod requests 2 GPUs via ResourceClaim
3. **Gang Scheduling + DRA**: 3 pods, each with 1 GPU, scheduled atomically

The mock driver sets environment variables (`GPU_DEVICE_0`, `GPU_DEVICE_1`, etc.) to simulate GPU allocation.

---

## GKE GPU Cluster Setup

Google Kubernetes Engine provides managed Kubernetes with GPU support.

### Free Tier Limitations

**Important**: GKE does **NOT** offer GPU nodes in the free tier. GPU usage incurs charges:

- NVIDIA Tesla T4: ~$0.35/hour
- NVIDIA Tesla V100: ~$2.48/hour
- NVIDIA A100: ~$3.67/hour

**Always Ready Free Tier**:
- 1 e2-micro instance per month (non-GPU)
- For testing, use preemptible GPU nodes to reduce costs by ~80%

### Create GPU-Enabled GKE Cluster

```bash
export PROJECT_ID="your-gcp-project"
export CLUSTER_NAME="kubenexus-gpu-test"
export REGION="us-central1"
export ZONE="us-central1-a"

# Authenticate
gcloud auth login
gcloud config set project $PROJECT_ID

# Create cluster with GPU node pool (PREEMPTIBLE for cost savings)
gcloud container clusters create $CLUSTER_NAME \
  --zone=$ZONE \
  --machine-type=n1-standard-4 \
  --num-nodes=1 \
  --enable-autoscaling \
  --min-nodes=1 \
  --max-nodes=3 \
  --release-channel=rapid \
  --cluster-version=1.35

# Add GPU node pool with NVIDIA Tesla T4 (cheapest GPU option)
gcloud container node-pools create gpu-pool \
  --cluster=$CLUSTER_NAME \
  --zone=$ZONE \
  --machine-type=n1-standard-4 \
  --accelerator=type=nvidia-tesla-t4,count=1 \
  --num-nodes=1 \
  --preemptible \
  --enable-autoscaling \
  --min-nodes=0 \
  --max-nodes=2

# Get credentials
gcloud container clusters get-credentials $CLUSTER_NAME --zone=$ZONE
```

### Install NVIDIA GPU Driver

GKE requires the NVIDIA GPU driver DaemonSet:

```bash
# Install NVIDIA device plugin
kubectl apply -f https://raw.githubusercontent.com/GoogleCloudPlatform/container-engine-accelerators/master/nvidia-driver-installer/cos/daemonset-preloaded-latest.yaml

# Verify GPU nodes
kubectl get nodes -o json | jq '.items[] | select(.status.capacity."nvidia.com/gpu" != null) | {name: .metadata.name, gpus: .status.capacity."nvidia.com/gpu"}'
```

### Deploy kubenexus-scheduler

```bash
# Load scheduler image (build locally, push to GCR)
docker build -t gcr.io/$PROJECT_ID/kubenexus-scheduler:latest -f Dockerfile.simple .
docker push gcr.io/$PROJECT_ID/kubenexus-scheduler:latest

# Update deployment to use GCR image
sed -i "s|kubenexus-scheduler:latest|gcr.io/$PROJECT_ID/kubenexus-scheduler:latest|" deploy/kubenexus-scheduler.yaml

# Deploy
kubectl apply -f deploy/kubenexus-scheduler.yaml

# Verify
kubectl get pods -n kubenexus-system
```

### Test GPU Workloads

```bash
# Create test namespace
kubectl create namespace gpu-test

# Example GPU pod using NVIDIA runtime
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-test-pod
  namespace: gpu-test
spec:
  schedulerName: kubenexus-scheduler
  restartPolicy: Never
  containers:
  - name: cuda-test
    image: nvidia/cuda:12.2.0-base-ubuntu22.04
    command: ["nvidia-smi"]
    resources:
      limits:
        nvidia.com/gpu: 1
EOF

# Check logs
kubectl logs gpu-test-pod -n gpu-test
```

### Cost Management

**CRITICAL**: Always clean up GPU resources to avoid charges:

```bash
# Delete GPU node pool (stops GPU billing)
gcloud container node-pools delete gpu-pool --cluster=$CLUSTER_NAME --zone=$ZONE

# Delete entire cluster
gcloud container clusters delete $CLUSTER_NAME --zone=$ZONE
```

**Cost Estimates** (as of 2024):
- Preemptible T4 GPU: ~$0.11/hour
- n1-standard-4: ~$0.19/hour
- **Total**: ~$0.30/hour or **~$7/day**

**Budget Protection**:
```bash
# Set budget alert
gcloud billing budgets create \
  --billing-account=YOUR_BILLING_ACCOUNT \
  --display-name="GPU Testing Budget" \
  --budget-amount=50USD \
  --threshold-rule=percent=50 \
  --threshold-rule=percent=90 \
  --threshold-rule=percent=100
```

---

## EKS GPU Cluster Setup

Amazon Elastic Kubernetes Service with GPU instances.

### Free Tier Limitations

**Important**: EKS does **NOT** offer GPU instances in the free tier. GPU usage incurs charges:

- g4dn.xlarge (T4 GPU): ~$0.526/hour
- p3.2xlarge (V100 GPU): ~$3.06/hour
- p4d.24xlarge (A100 GPU): ~$32.77/hour

**AWS Free Tier**:
- 750 hours/month of t2.micro or t3.micro (12 months, non-GPU)
- EKS control plane: $0.10/hour (NOT free)

For cost-effective testing, use **Spot Instances** (up to 90% discount).

### Create GPU-Enabled EKS Cluster

```bash
export CLUSTER_NAME="kubenexus-gpu-test"
export REGION="us-west-2"

# Install eksctl
# macOS: brew install eksctl
# Linux: See https://eksctl.io/installation/

# Create cluster with GPU node group (SPOT instances)
eksctl create cluster \
  --name=$CLUSTER_NAME \
  --region=$REGION \
  --version=1.35 \
  --nodegroup-name=cpu-nodes \
  --node-type=t3.medium \
  --nodes=2 \
  --nodes-min=1 \
  --nodes-max=3 \
  --managed

# Add GPU node group with NVIDIA T4 (g4dn.xlarge)
eksctl create nodegroup \
  --cluster=$CLUSTER_NAME \
  --region=$REGION \
  --name=gpu-nodes \
  --node-type=g4dn.xlarge \
  --nodes=1 \
  --nodes-min=0 \
  --nodes-max=2 \
  --spot \
  --instance-types=g4dn.xlarge

# Get credentials
aws eks update-kubeconfig --region $REGION --name $CLUSTER_NAME
```

### Install NVIDIA GPU Driver

EKS requires the NVIDIA device plugin:

```bash
# Install NVIDIA device plugin
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.16.2/deployments/static/nvidia-device-plugin.yml

# Verify GPU nodes
kubectl get nodes "-o=custom-columns=NAME:.metadata.name,GPU:.status.allocatable.nvidia\.com/gpu"
```

### Deploy kubenexus-scheduler

```bash
# Push scheduler image to ECR
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export ECR_REPO="$AWS_ACCOUNT_ID.dkr.ecr.$REGION.amazonaws.com/kubenexus-scheduler"

# Create ECR repository
aws ecr create-repository --repository-name kubenexus-scheduler --region $REGION || true

# Authenticate Docker to ECR
aws ecr get-login-password --region $REGION | docker login --username AWS --password-stdin $ECR_REPO

# Build and push
docker build -t $ECR_REPO:latest -f Dockerfile.simple .
docker push $ECR_REPO:latest

# Update deployment
sed -i "s|kubenexus-scheduler:latest|$ECR_REPO:latest|" deploy/kubenexus-scheduler.yaml

# Deploy
kubectl apply -f deploy/kubenexus-scheduler.yaml

# Verify
kubectl get pods -n kubenexus-system
```

### Test GPU Workloads

```bash
# Create test namespace
kubectl create namespace gpu-test

# Example GPU pod
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-test-pod
  namespace: gpu-test
spec:
  schedulerName: kubenexus-scheduler
  restartPolicy: Never
  containers:
  - name: cuda-test
    image: nvidia/cuda:12.2.0-base-ubuntu22.04
    command: ["nvidia-smi"]
    resources:
      limits:
        nvidia.com/gpu: 1
EOF

# Check logs
kubectl logs gpu-test-pod -n gpu-test
```

### Cost Management

**CRITICAL**: Always clean up GPU resources:

```bash
# Delete GPU node group (stops GPU billing)
eksctl delete nodegroup --cluster=$CLUSTER_NAME --name=gpu-nodes --region=$REGION

# Delete entire cluster
eksctl delete cluster --name=$CLUSTER_NAME --region=$REGION
```

**Cost Estimates** (as of 2024):
- EKS control plane: $0.10/hour = ~$73/month
- Spot g4dn.xlarge: ~$0.16/hour (vs $0.526 on-demand)
- **Total**: ~$0.26/hour or **~$6/day** (with Spot)

**Budget Protection**:
```bash
# Set up AWS Budget
aws budgets create-budget \
  --account-id $AWS_ACCOUNT_ID \
  --budget file://budget.json
```

---

## Testing Workflow

### 1. Local Development (Free)

```bash
# Test with mock GPUs
./hack/install-dra-driver.sh
./test/e2e/scripts/run-dra-gpu-test.sh

# Verify gang scheduling + DRA integration
kubectl logs -n kubenexus-system deployment/kubenexus-scheduler | grep "gpu-gang"
```

### 2. Cloud Validation (Paid)

Once local testing passes, validate on real GPU cluster:

**GKE**:
```bash
# Create cluster (~$0.30/hour)
gcloud container clusters create ... --preemptible
# Run tests
kubectl apply -f test/e2e/fixtures/gpu-workload.yaml
# IMMEDIATELY delete after testing
gcloud container node-pools delete gpu-pool ...
```

**EKS**:
```bash
# Create cluster (~$0.26/hour)
eksctl create nodegroup ... --spot
# Run tests
kubectl apply -f test/e2e/fixtures/gpu-workload.yaml
# IMMEDIATELY delete after testing
eksctl delete nodegroup ...
```

### 3. Production Deployment

For production, consider:
- **GKE Autopilot**: Managed, auto-scales GPU nodes
- **EKS with Karpenter**: Automated node provisioning
- **Committed Use Discounts**: 1-year commitments save 37-55%

---

## Free Tier Alternatives

If you need truly free GPU testing:

### Option 1: Google Colab (Free)
- Limited GPU time (T4, free tier)
- Can install minikube/kind
- Good for quick validation

### Option 2: Kaggle Notebooks (Free)
- 30 hours/week of GPU time
- Can run Kind clusters
- Good for intermittent testing

### Option 3: University/Research Credits
- Google Cloud Research Credits
- AWS Educate Credits
- Azure for Students

### Option 4: GitHub Codespaces
- 120 core-hours/month free
- No GPU, but can test DRA mock driver

---

## Summary

| Platform | GPU Cost | Free Tier | Best For |
|----------|----------|-----------|----------|
| **Kind + DRA Mock** | $0 | Unlimited | Development, CI/CD |
| **GKE + Preemptible** | ~$0.30/hr | None | Real GPU validation |
| **EKS + Spot** | ~$0.26/hr | None | Real GPU validation |
| **Colab/Kaggle** | $0 | Limited | Quick tests |

**Recommendation**: 
1. Develop and test with **DRA mock driver** (free, unlimited)
2. Final validation on **GKE/EKS Spot/Preemptible** (1-2 hours, ~$0.50 total)
3. Set budget alerts and delete resources immediately after testing

---

## Next Steps

1. **Complete local testing**: `./test/e2e/scripts/run-dra-gpu-test.sh`
2. **Choose cloud provider**: GKE (simpler) or EKS (more flexible)
3. **Set budget alerts**: Prevent surprise charges
4. **Run validation tests**: 1-2 hours on real GPUs
5. **Clean up immediately**: Delete GPU node pools

For questions or issues, see:
- [DRA Example Driver](https://github.com/kubernetes-sigs/dra-example-driver)
- [GKE GPU Documentation](https://cloud.google.com/kubernetes-engine/docs/how-to/gpus)
- [EKS GPU Documentation](https://docs.aws.amazon.com/eks/latest/userguide/gpu-ami.html)
