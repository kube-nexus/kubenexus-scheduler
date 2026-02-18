---
name: Bug report
about: Create a report to help us improve
title: '[BUG] '
labels: bug
assignees: ''
---

## Describe the bug
A clear and concise description of what the bug is.

## To Reproduce
Steps to reproduce the behavior:
1. Deploy KubeNexus scheduler with '...'
2. Create pod with labels '...'
3. Observe '...'
4. See error

## Expected behavior
A clear and concise description of what you expected to happen.

## Actual behavior
What actually happened.

## Environment
- KubeNexus version: [e.g., v0.1.0]
- Kubernetes version: [e.g., 1.28.5]
- Deployment mode: [single-replica / HA]
- Cloud provider: [e.g., AWS EKS, GKE, on-prem]

## Pod/Workload Configuration
```yaml
# Paste your pod/job YAML here
```

## Scheduler Logs
```
# Paste relevant scheduler logs here
kubectl logs -n kube-system deployment/kubenexus-scheduler
```

## Additional context
Add any other context about the problem here (screenshots, metrics, etc.)
