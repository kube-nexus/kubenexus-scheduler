# NUMATopology Plugin - Node Labeling Guide

## Overview

The NUMATopology plugin requires nodes to be labeled with NUMA topology information. This guide explains how to set up node labels for NUMA-aware scheduling.

## Required Node Labels

For each node with multiple NUMA nodes, the following labels must be set:

```yaml
# Number of NUMA nodes on this server
numa.kubenexus.io/node-count: "2"

# For each NUMA node (0, 1, 2, ...):
numa.kubenexus.io/node-0-cpus: "0-15,32-47"      # CPU list (ranges or singles)
numa.kubenexus.io/node-0-memory: "68719476736"   # Memory in bytes (64GB)

numa.kubenexus.io/node-1-cpus: "16-31,48-63"
numa.kubenexus.io/node-1-memory: "68719476736"
```

## How to Label Nodes

### Method 1: Manual Labeling (Testing)

```bash
# Label a node with 2 NUMA nodes
kubectl label node worker-1 \
  numa.kubenexus.io/node-count=2 \
  numa.kubenexus.io/node-0-cpus=0-15,32-47 \
  numa.kubenexus.io/node-0-memory=68719476736 \
  numa.kubenexus.io/node-1-cpus=16-31,48-63 \
  numa.kubenexus.io/node-1-memory=68719476736
```

### Method 2: DaemonSet Node Labeler (Production)

Create a DaemonSet that automatically detects and labels NUMA topology:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: numa-labeler
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: numa-labeler
  template:
    metadata:
      labels:
        app: numa-labeler
    spec:
      hostPID: true
      containers:
      - name: labeler
        image: kubenexus/numa-labeler:latest
        securityContext:
          privileged: true
        volumeMounts:
        - name: sys
          mountPath: /host/sys
          readOnly: true
      volumes:
      - name: sys
        hostPath:
          path: /sys
```

The labeler script reads from `/sys/devices/system/node/` to detect NUMA topology.

### Method 3: Using `numactl` Command

Get NUMA information from a node:

```bash
# SSH into node
ssh worker-1

# Show NUMA topology
numactl --hardware

# Output:
# available: 2 nodes (0-1)
# node 0 cpus: 0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47
# node 0 size: 65536 MB
# node 0 free: 45231 MB
# node 1 cpus: 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 48 49 50 51 52 53 54 55 56 57 58 59 60 61 62 63
# node 1 size: 65536 MB
# node 1 free: 50123 MB
```

Then label based on output:

```bash
kubectl label node worker-1 \
  numa.kubenexus.io/node-count=2 \
  numa.kubenexus.io/node-0-cpus="0-15,32-47" \
  numa.kubenexus.io/node-0-memory="68719476736" \
  numa.kubenexus.io/node-1-cpus="16-31,48-63" \
  numa.kubenexus.io/node-1-memory="68719476736"
```

## Detecting NUMA Topology

### On Linux (most common):

```bash
# Check if NUMA is available
ls /sys/devices/system/node/

# Should show: node0, node1, node2, etc.

# Get CPUs for each NUMA node
cat /sys/devices/system/node/node0/cpulist
# Output: 0-15,32-47

cat /sys/devices/system/node/node1/cpulist
# Output: 16-31,48-63

# Get memory for each NUMA node
numactl --hardware | grep "node 0 size"
# Output: node 0 size: 65536 MB
```

### Converting Memory to Bytes:

```bash
# MB to bytes
echo $((65536 * 1024 * 1024))
# Output: 68719476736
```

## Example Node Topologies

### Single Socket (No NUMA)
```yaml
# No labels needed - plugin will allow scheduling
```

### Dual Socket Server (2 NUMA nodes)
```yaml
numa.kubenexus.io/node-count: "2"
numa.kubenexus.io/node-0-cpus: "0-15,32-47"
numa.kubenexus.io/node-0-memory: "68719476736"
numa.kubenexus.io/node-1-cpus: "16-31,48-63"
numa.kubenexus.io/node-1-memory: "68719476736"
```

### Quad Socket Server (4 NUMA nodes)
```yaml
numa.kubenexus.io/node-count: "4"
numa.kubenexus.io/node-0-cpus: "0-7,32-39"
numa.kubenexus.io/node-0-memory: "34359738368"
numa.kubenexus.io/node-1-cpus: "8-15,40-47"
numa.kubenexus.io/node-1-memory: "34359738368"
numa.kubenexus.io/node-2-cpus: "16-23,48-55"
numa.kubenexus.io/node-2-memory: "34359738368"
numa.kubenexus.io/node-3-cpus: "24-31,56-63"
numa.kubenexus.io/node-3-memory: "34359738368"
```

## Verification

Check if labels are applied correctly:

```bash
# Show NUMA labels for a node
kubectl get node worker-1 -o json | jq '.metadata.labels | with_entries(select(.key | startswith("numa")))'

# Expected output:
# {
#   "numa.kubenexus.io/node-0-cpus": "0-15,32-47",
#   "numa.kubenexus.io/node-0-memory": "68719476736",
#   "numa.kubenexus.io/node-1-cpus": "16-31,48-63",
#   "numa.kubenexus.io/node-1-memory": "68719476736",
#   "numa.kubenexus.io/node-count": "2"
# }
```

## Troubleshooting

### Plugin Not Filtering Based on NUMA

1. Check if node has labels:
   ```bash
   kubectl describe node <node-name> | grep numa
   ```

2. Check if pod is batch/ML workload:
   - Plugin only applies strict NUMA for batch workloads
   - Or pods with `scheduling.kubenexus.io/numa-policy: "single-numa-node"`

3. Check scheduler logs:
   ```bash
   kubectl logs -n kube-system <scheduler-pod> | grep NUMATopology
   ```

### Node Has NUMA but No Labels

- Install the NUMA labeler DaemonSet
- Or manually label nodes using the commands above

### Incorrect CPU List Format

- Use ranges: `0-7` not `0,1,2,3,4,5,6,7`
- Use commas for multiple ranges: `0-7,16-23`
- No spaces: `0-7,16-23` not `0-7, 16-23`

## Best Practices

1. **Automate Labeling**: Use DaemonSet for production
2. **Update on Node Changes**: Re-label if hardware changes
3. **Monitor NUMA Metrics**: Track cross-NUMA access
4. **Test Before Prod**: Verify labels on test nodes first
5. **Document Topology**: Keep inventory of node NUMA layouts

## See Also

- [NUMA Plugin Configuration](../README.md#numatopology)
- [Kubelet Topology Manager](https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/)
- [Pod NUMA Annotations](./POD_NUMA_USAGE.md)
