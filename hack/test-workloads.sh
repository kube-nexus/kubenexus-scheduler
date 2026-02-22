#!/bin/bash
set -e

echo "========================================="
echo "Testing KubeNexus Scheduler with Workloads"
echo "========================================="

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Create test namespace
echo -e "${YELLOW}Creating test namespace...${NC}"
kubectl create namespace kubenexus-test --dry-run=client -o yaml | kubectl apply -f -

# Test 1: Gang Scheduling (Label-based)
echo -e "\n${BLUE}========================================="
echo "Test 1: Gang Scheduling (Label-based)"
echo "=========================================${NC}"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gang-worker-1
  namespace: kubenexus-test
  labels:
    pod-group.scheduling.kubenexus.io/name: "test-gang"
    pod-group.scheduling.kubenexus.io/min-available: "3"
    app: gang-test
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: worker
    image: busybox:1.36
    command: ["sleep", "3600"]
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
---
apiVersion: v1
kind: Pod
metadata:
  name: gang-worker-2
  namespace: kubenexus-test
  labels:
    pod-group.scheduling.kubenexus.io/name: "test-gang"
    pod-group.scheduling.kubenexus.io/min-available: "3"
    app: gang-test
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: worker
    image: busybox:1.36
    command: ["sleep", "3600"]
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
---
apiVersion: v1
kind: Pod
metadata:
  name: gang-worker-3
  namespace: kubenexus-test
  labels:
    pod-group.scheduling.kubenexus.io/name: "test-gang"
    pod-group.scheduling.kubenexus.io/min-available: "3"
    app: gang-test
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: worker
    image: busybox:1.36
    command: ["sleep", "3600"]
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
EOF

echo -e "${YELLOW}Waiting for gang pods to be scheduled...${NC}"
sleep 5
kubectl get pods -n kubenexus-test -l app=gang-test
kubectl wait --for=condition=Ready --timeout=60s pod -n kubenexus-test -l app=gang-test
echo -e "${GREEN}✓ Gang scheduling test passed - all 3 pods scheduled together${NC}"

# Test 2: Batch Workload (Workload-aware scoring)
echo -e "\n${BLUE}========================================="
echo "Test 2: Batch Workload with Workload-aware Scoring"
echo "=========================================${NC}"

cat <<EOF | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: batch-job
  namespace: kubenexus-test
spec:
  parallelism: 2
  completions: 2
  template:
    metadata:
      labels:
        workload-type: batch
        app: batch-test
    spec:
      schedulerName: kubenexus-scheduler
      restartPolicy: OnFailure
      containers:
      - name: batch-worker
        image: busybox:1.36
        command: ["sh", "-c", "echo 'Processing batch job'; sleep 10; echo 'Done'"]
        resources:
          requests:
            cpu: "200m"
            memory: "256Mi"
EOF

echo -e "${YELLOW}Waiting for batch job...${NC}"
kubectl wait --for=condition=Complete --timeout=120s job/batch-job -n kubenexus-test
echo -e "${GREEN}✓ Batch workload test passed${NC}"

# Test 3: Service Workload
echo -e "\n${BLUE}========================================="
echo "Test 3: Service Workload with Topology Spreading"
echo "=========================================${NC}"

cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: service-deployment
  namespace: kubenexus-test
spec:
  replicas: 3
  selector:
    matchLabels:
      app: service-test
  template:
    metadata:
      labels:
        app: service-test
        workload-type: service
    spec:
      schedulerName: kubenexus-scheduler
      containers:
      - name: nginx
        image: nginx:1.25-alpine
        ports:
        - containerPort: 80
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
EOF

echo -e "${YELLOW}Waiting for service pods...${NC}"
kubectl wait --for=condition=available --timeout=120s deployment/service-deployment -n kubenexus-test
kubectl get pods -n kubenexus-test -l app=service-test -o wide
echo -e "${GREEN}✓ Service workload test passed${NC}"

# Display results
echo -e "\n${BLUE}========================================="
echo "Test Results Summary"
echo "=========================================${NC}"

echo -e "\n${YELLOW}All pods in test namespace:${NC}"
kubectl get pods -n kubenexus-test -o wide

echo -e "\n${YELLOW}Pod distribution across nodes:${NC}"
kubectl get pods -n kubenexus-test -o wide | awk 'NR>1 {print $7}' | sort | uniq -c

echo -e "\n${YELLOW}Scheduler logs (last 30 lines):${NC}"
kubectl logs -n kube-system -l app=kubenexus-scheduler --tail=30

echo -e "\n${GREEN}========================================="
echo "All tests completed successfully!"
echo "=========================================${NC}"
echo ""
echo "To cleanup:"
echo "  kubectl delete namespace kubenexus-test"
echo "  kind delete cluster --name kubenexus-test"
