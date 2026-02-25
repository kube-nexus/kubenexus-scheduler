# KubeNexus Admission Webhook

## Overview

The KubeNexus Admission Webhook provides **deterministic autoscaling** by automatically injecting `nodeSelector` constraints based on tenant tier. This ensures cluster autoscaler knows exactly which GPU nodepool to scale.

## Problem It Solves

**Without Webhook:**
```yaml
# Pod submitted to Bronze tier namespace
spec:
  containers:
  - resources:
      limits:
        nvidia.com/gpu: 1
  # No nodeSelector → could land on L4, A100, OR H100
  # Cluster autoscaler doesn't know which nodepool to scale
  # Result: Non-deterministic, expensive mistakes
```

**With Webhook:**
```yaml
# Webhook automatically injects nodeSelector
spec:
  nodeSelector:
    gpu.nvidia.com/class: l4  # ← Bronze tier MUST use L4
  containers:
  - resources:
      limits:
        nvidia.com/gpu: 1
  # Cluster autoscaler sees l4 constraint → scales L4 nodepool
  # Result: Deterministic, cost-controlled autoscaling
```

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│ 1. User submits Job to namespace with tier label            │
│    Namespace: ml-team-dev                                    │
│    Label: tenant.kubenexus.io/tier=bronze                    │
└──────────────────────────────────────────────────────────────┘
                           ↓
┌──────────────────────────────────────────────────────────────┐
│ 2. Kubernetes API Server intercepts Pod CREATE              │
│    Calls: kubenexus-webhook.kube-system.svc/mutate-pod      │
└──────────────────────────────────────────────────────────────┘
                           ↓
┌──────────────────────────────────────────────────────────────┐
│ 3. Webhook reads namespace tier and injects nodeSelector    │
│    - Gets namespace: ml-team-dev                             │
│    - Reads tier: bronze                                      │
│    - Maps bronze → GPU class: l4                             │
│    - Injects: spec.nodeSelector["gpu.nvidia.com/class"]=l4  │
└──────────────────────────────────────────────────────────────┘
                           ↓
┌──────────────────────────────────────────────────────────────┐
│ 4. Pod created with deterministic node constraint           │
│    Pod: nodeSelector: {gpu.nvidia.com/class: l4}            │
│    Status: Pending (waits for L4 node)                      │
└──────────────────────────────────────────────────────────────┘
                           ↓
┌──────────────────────────────────────────────────────────────┐
│ 5. Cluster Autoscaler scales L4 nodepool                    │
│    Sees: nodeSelector requires L4 → scales L4 nodes         │
└──────────────────────────────────────────────────────────────┘
```

---

## Tier to GPU Class Mapping

| Tenant Tier | GPU Class | Hardware | Use Case | Cost |
|-------------|-----------|----------|----------|------|
| **Gold** | `h100` | NVIDIA H100 | Large model training, frontier research | $$$$$ |
| **Silver** | `a100` | NVIDIA A100 | Production training, fine-tuning | $$$ |
| **Bronze** | `l4` | NVIDIA L4 | Inference, small models, dev/test | $$ |

### Why This Mapping?

1. **Cost Control**: Bronze tenants cannot accidentally use expensive H100 nodes
2. **Performance Guarantee**: Gold tenants get best hardware
3. **Deterministic Autoscaling**: Each tier maps to ONE GPU class
4. **Resource Isolation**: Physical separation of tier workloads

---

## Setup

### Prerequisites

1. Kubernetes cluster with GPU nodes labeled by class:
   ```bash
   # Label H100 nodes
   kubectl label nodes gpu-h100-node-1 gpu.nvidia.com/class=h100
   
   # Label A100 nodes
   kubectl label nodes gpu-a100-node-1 gpu.nvidia.com/class=a100
   
   # Label L4 nodes
   kubectl label nodes gpu-l4-node-1 gpu.l4-node-2 gpu.nvidia.com/class=l4
   ```

2. Namespaces labeled with tenant tier:
   ```bash
   # Gold tier namespace
   kubectl create namespace ml-team-prod
   kubectl label namespace ml-team-prod tenant.kubenexus.io/tier=gold
   
   # Silver tier namespace
   kubectl create namespace ml-team-staging
   kubectl label namespace ml-team-staging tenant.kubenexus.io/tier=silver
   
   # Bronze tier namespace
   kubectl create namespace ml-team-dev
   kubectl label namespace ml-team-dev tenant.kubenexus.io/tier=bronze
   ```

### Installation

#### Step 1: Generate TLS Certificates

```bash
# Generate self-signed certs (for dev/test)
make generate-webhook-certs

# For production, use cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
```

#### Step 2: Build and Push Webhook Image

```bash
# Build image
make docker-build-webhook

# Tag and push to your registry
docker tag kubenexus-webhook:v0.1.0 your-registry/kubenexus-webhook:v0.1.0
docker push your-registry/kubenexus-webhook:v0.1.0

# Update deploy/webhook.yaml with your image
sed -i 's|image: kubenexus-webhook:latest|image: your-registry/kubenexus-webhook:v0.1.0|' deploy/webhook.yaml
```

#### Step 3: Deploy Webhook

```bash
# Apply webhook deployment
kubectl apply -f deploy/webhook-configured.yaml

# Verify webhook is running
kubectl get pods -n kube-system -l app=kubenexus-webhook
kubectl get mutatingwebhookconfiguration kubenexus-webhook
```

---

## Usage Examples

### Example 1: Bronze Tier Training Job

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: training-job
  namespace: ml-team-dev  # Bronze tier namespace
spec:
  template:
    spec:
      containers:
      - name: trainer
        image: pytorch/pytorch:latest
        resources:
          limits:
            nvidia.com/gpu: 1
        # No nodeSelector specified by user
      restartPolicy: Never
```

**After webhook mutation:**
```yaml
spec:
  nodeSelector:
    gpu.nvidia.com/class: l4  # ← Automatically injected
  containers:
  - name: trainer
    resources:
      limits:
        nvidia.com/gpu: 1
```

**Result:**
- Pod MUST land on L4 nodes
- Cluster autoscaler scales L4 nodepool if needed
- Cost-controlled (cannot use H100)

### Example 2: Gold Tier Large Model Training

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: llama-training
  namespace: ml-research  # Gold tier namespace
spec:
  template:
    spec:
      containers:
      - name: trainer
        image: nvcr.io/nvidia/pytorch:24.01-py3
        resources:
          limits:
            nvidia.com/gpu: 8
```

**After webhook mutation:**
```yaml
spec:
  nodeSelector:
    gpu.nvidia.com/class: h100  # ← Gold → H100
  containers:
  - name: trainer
    resources:
      limits:
        nvidia.com/gpu: 8
```

**Result:**
- Pod lands on H100 8-GPU nodes
- Cluster autoscaler scales H100 nodepool
- Best performance for frontier research

### Example 3: User Override (Skip Webhook)

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: special-pod
  namespace: ml-team-dev  # Bronze tier
spec:
  nodeSelector:
    gpu.nvidia.com/class: a100  # ← User explicitly requests A100
  containers:
  - name: app
    resources:
      limits:
        nvidia.com/gpu: 1
```

**Webhook behavior:**
- Sees existing `gpu.nvidia.com/class` nodeSelector
- **Skips mutation** (respects user override)
- Pod can land on A100 if user explicitly requests it

**Use case:** Admin override for special workloads

---

## Testing

### Unit Tests

```bash
# Run webhook tests
make test-webhook

# Output:
# === RUN   TestGetTierGPUClass
# === RUN   TestRequestsGPUs
# === RUN   TestMutateEndToEnd
# PASS
```

### Integration Test

```bash
# 1. Deploy webhook to test cluster
make deploy-webhook

# 2. Create test namespace
kubectl create namespace webhook-test
kubectl label namespace webhook-test tenant.kubenexus.io/tier=bronze

# 3. Submit test pod
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-test-pod
  namespace: webhook-test
spec:
  containers:
  - name: cuda
    image: nvidia/cuda:12.0-base
    resources:
      limits:
        nvidia.com/gpu: 1
    command: ["nvidia-smi"]
EOF

# 4. Verify nodeSelector was injected
kubectl get pod gpu-test-pod -n webhook-test -o yaml | grep -A2 nodeSelector

# Expected output:
#   nodeSelector:
#     gpu.nvidia.com/class: l4
```

---

## Troubleshooting

### Webhook Not Mutating Pods

**Check 1: Webhook is running**
```bash
kubectl get pods -n kube-system -l app=kubenexus-webhook
# Should show 2/2 Running
```

**Check 2: Namespace has tier label**
```bash
kubectl get namespace <your-namespace> --show-labels
# Should have: tenant.kubenexus.io/tier=<gold|silver|bronze>
```

**Check 3: Pod requests GPUs**
```bash
# Webhook only mutates GPU pods
kubectl get pod <pod-name> -o yaml | grep "nvidia.com/gpu"
```

**Check 4: Webhook logs**
```bash
kubectl logs -n kube-system -l app=kubenexus-webhook -f
# Look for: "Injecting GPU class nodeSelector"
```

### Certificate Issues

**Error:** `x509: certificate signed by unknown authority`

**Solution:**
```bash
# Regenerate certificates
make generate-webhook-certs

# Update webhook configuration
kubectl delete mutatingwebhookconfiguration kubenexus-webhook
kubectl apply -f deploy/webhook-configured.yaml
```

### Admission Failures

**Error:** `Internal error occurred: failed calling webhook`

**Check webhook connectivity:**
```bash
# From inside cluster
kubectl run -it test --image=curlimages/curl -- sh
curl -k https://kubenexus-webhook.kube-system.svc:443/healthz
# Should return: ok
```

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `--port` | `8443` | Webhook server HTTPS port |
| `--tls-cert-file` | `/etc/webhook/certs/tls.crt` | TLS certificate path |
| `--tls-key-file` | `/etc/webhook/certs/tls.key` | TLS private key path |
| `--v` | `2` | Log verbosity (0-9) |

### Webhook Configuration

```yaml
# deploy/webhook.yaml
webhooks:
- name: pod-mutation.kubenexus.io
  failurePolicy: Ignore  # ← Allow pods if webhook fails
  timeoutSeconds: 5
  namespaceSelector:
    matchExpressions:
    - key: tenant.kubenexus.io/tier
      operator: Exists  # Only mutate namespaces with tier label
```

**Failure Policy Options:**
- `Ignore`: Allow pod creation if webhook fails (recommended)
- `Fail`: Reject pod if webhook fails (stricter)

---

## Security Considerations

### TLS Certificate Management

**Development:** Self-signed certificates (hack/generate-webhook-certs.sh)

**Production:** Use cert-manager
```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: kubenexus-webhook-cert
  namespace: kube-system
spec:
  secretName: kubenexus-webhook-certs
  issuerRef:
    name: selfsigned-issuer
    kind: ClusterIssuer
  dnsNames:
  - kubenexus-webhook.kube-system.svc
  - kubenexus-webhook.kube-system.svc.cluster.local
```

### RBAC

Webhook requires:
- `get`, `list`, `watch` on `namespaces` (to read tier labels)
- No write permissions (read-only)

```yaml
# See deploy/webhook.yaml for full RBAC
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubenexus-webhook
rules:
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list", "watch"]
```

---

## Performance

### Latency

- **Typical:** 2-5ms per pod creation
- **Worst case:** 10ms (namespace API call)
- **Impact:** Negligible (webhook runs async)

### Resource Usage

- **CPU:** 10-50m (under load)
- **Memory:** 50-100Mi
- **Replicas:** 2 (HA configuration)

### Scaling

- Webhook is stateless → can scale horizontally
- Each replica handles ~1000 requests/sec
- Kubernetes load-balances across replicas

---

## Roadmap

- [ ] Support for multi-GPU class preferences (e.g., "a100 or h100")
- [ ] Dynamic tier-to-GPU mapping via ConfigMap
- [ ] Metrics/observability (Prometheus metrics)
- [ ] Workload-specific overrides (training vs inference)
- [ ] Integration with Kueue ResourceFlavors

---

## Related Documentation

- [ProfileClassifier Plugin](./PROFILE_CLASSIFIER.md) - Scheduling-time classification
- [TenantHardware Plugin](./TENANT_HARDWARE.md) - Tier-to-hardware matching
- [GPU Topology Implementation](./GPU_TOPOLOGY_IMPLEMENTATION.md) - Multi-GPU placement
- [Deterministic Autoscaling Design](./DESIGN_DECISIONS.md#deterministic-autoscaling)

---

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for development guidelines.

**Testing checklist:**
- [ ] Unit tests pass (`make test-webhook`)
- [ ] Integration test on kind cluster
- [ ] Verify nodeSelector injection for each tier
- [ ] Check user override behavior
- [ ] Validate certificate generation
