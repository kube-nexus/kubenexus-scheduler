# Testing Quick Reference Guide

This guide provides quick commands for running all types of tests in the KubeNexus Scheduler project.

## Prerequisites

### Required Tools
- **Go 1.25+**: `go version`
- **Make**: `make --version`
- **Docker**: `docker --version` (for E2E tests)
- **kubectl**: `kubectl version --client` (for E2E tests)
- **Kind**: `kind version` (for E2E tests)

### Installing Kind (for E2E tests)
```bash
go install sigs.k8s.io/kind@latest
```

---

## Quick Commands

### Build & Formatting
```bash
# Build the scheduler binary
make build

# Format code
make fmt

# Build Docker image
make docker-build
```

### Linting
```bash
# Run all linters
make lint

# The linter config is in .golangci.yml
```

### Unit Tests
```bash
# Run all unit tests
make test

# Run unit tests with verbose output
go test -v ./pkg/...

# Run tests for a specific package
go test -v ./pkg/plugins/coscheduling/

# Run specific test
go test -v ./pkg/workload/ -run TestClassifyPod_SparkJob

# Run with coverage
go test -cover ./pkg/...

# Generate coverage report
go test -coverprofile=coverage.out ./pkg/...
go tool cover -html=coverage.out
```

### E2E Tests
```bash
# Run E2E tests (creates Kind cluster, builds image, deploys, tests)
make test-e2e

# E2E tests require Kind installed locally
# They will:
# 1. Create a Kind cluster named 'kubenexus-e2e'
# 2. Build the scheduler Docker image
# 3. Load the image into Kind
# 4. Deploy the scheduler using kubectl
# 5. Wait for scheduler pod to be ready
# 6. Run verification tests
# 7. Clean up the cluster
```

### Benchmarks
```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run benchmarks for specific package
go test -bench=. -benchmem ./pkg/plugins/coscheduling/
```

### Clean Up
```bash
# Remove build artifacts
make clean

# Delete Kind cluster (if E2E test didn't clean up)
kind delete cluster --name kubenexus-e2e
```

---

## Common Workflows

### Before Committing
```bash
# Full validation before commit
make fmt
make lint
make test
make build
```

### Full Test Suite
```bash
# Run everything (requires Kind for E2E)
make fmt && make lint && make test && make test-e2e
```

### Quick Development Loop
```bash
# Fast iteration during development
make build && make test
```

### Debugging E2E Failures
```bash
# Run E2E tests with more verbose output
go test -v ./test/e2e/

# Keep the Kind cluster after test for debugging
# (modify test code to skip cleanup)

# Check scheduler logs in the cluster
kubectl logs -n kubenexus-system -l app=kubenexus-scheduler

# Check scheduler pod status
kubectl get pods -n kubenexus-system -l app=kubenexus-scheduler

# Describe scheduler pod
kubectl describe pod -n kubenexus-system -l app=kubenexus-scheduler
```

---

## Test Coverage by Package

### `pkg/apis/scheduling/v1alpha1`
- Type definitions and DeepCopy
- Constants validation

### `pkg/plugins/coscheduling`
- Plugin name and initialization
- Constants validation

### `pkg/plugins/resourcereservation`
- Plugin initialization
- Resource reservation creation
- Reserve/Unreserve operations
- Owner references
- Nil pod handling

### `pkg/workload`
- Pod classification for all frameworks:
  - Spark
  - TensorFlow
  - PyTorch
  - Ray
  - MPI
- Gang scheduling detection
- Batch vs. Service classification
- Edge cases (nil pods, empty labels, etc.)

### `pkg/utils`
- Pod group label extraction
- MinAvailable parsing
- Invalid input handling
- Large gang support

### `pkg/scheduler`
- No tests yet (metrics and types only)

---

## CI/CD Integration

### GitHub Actions Workflows

#### Main CI (`ci.yml`)
Runs on every push and PR to `main`:
```yaml
- Build verification
- Unit tests
- Linting
```

#### E2E Tests (`e2e.yml`)
Runs on PR, push, nightly, and manual trigger:
```yaml
- Kind cluster setup
- Scheduler build
- E2E test execution
```

#### Benchmarks (`benchmark.yml`)
Runs nightly and on manual trigger:
```yaml
- Benchmark execution
- Result archiving
```

### Checking CI Status
```bash
# Using GitHub CLI (if installed)
gh run list --branch test/add-testing-infrastructure

# Or visit GitHub Actions page in browser
# https://github.com/YOUR_ORG/kubenexus-scheduler/actions
```

---

## Troubleshooting

### "kind: command not found"
```bash
# Install Kind
go install sigs.k8s.io/kind@latest

# Make sure $GOPATH/bin is in your PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

### "permission denied" when running Docker
```bash
# Add user to docker group (Linux)
sudo usermod -aG docker $USER
newgrp docker

# Or use Docker Desktop (macOS/Windows)
```

### E2E tests timeout
```bash
# Increase timeout in test code
# The default is 5 minutes for cluster creation
# and 10 minutes for pod ready wait

# Or check if Docker is running slow
docker info

# Clean up old containers/images
docker system prune -a
```

### Linter reports errors not in CI
```bash
# Make sure you're using the same linter version
# CI uses v1.61.0 (specified in workflows)

# Install specific version
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.61.0
```

### Tests pass locally but fail in CI
```bash
# Check Go version matches CI
go version  # Should be 1.25

# Check for platform-specific issues
# (file paths, line endings, etc.)

# Run tests with race detector like CI does
go test -race ./pkg/...
```

---

## Test Writing Guidelines

### Unit Tests
- Place tests in `*_test.go` files in the same package
- Use table-driven tests for multiple scenarios
- Test both happy path and error cases
- Use subtests for organization: `t.Run("scenario", func(t *testing.T) {...})`
- Mock external dependencies (Kubernetes client, etc.)

### E2E Tests
- Place in `test/e2e/` directory
- Use Kind clusters for realistic environment
- Clean up resources after tests
- Add detailed logging for debugging
- Use reasonable timeouts (5-10 minutes max)
- Test realistic user scenarios

### Benchmarks
- Place in `*_test.go` files with `Benchmark` prefix
- Use `b.N` for loop iterations
- Reset timer after setup: `b.ResetTimer()`
- Use `b.ReportAllocs()` to track allocations
- Run multiple times for stable results

---

## Performance Tips

### Faster Unit Tests
```bash
# Run in parallel (default)
go test ./pkg/...

# Use cached results when code hasn't changed
go test ./pkg/...  # Second run uses cache

# Skip slow tests during development
go test -short ./pkg/...
```

### Faster E2E Tests
```bash
# Reuse Kind cluster between test runs
# (modify test to skip cluster creation if exists)

# Use local image registry to avoid rebuilding
# (advanced setup, see Kind docs)
```

---

## Additional Resources

- **Main Documentation**: `README.md`
- **Architecture Guide**: `docs/architecture.md`
- **Contributing Guide**: `CONTRIBUTING.md`
- **CI/CD Status**: `docs/CI_CD_STATUS.md`
- **Scheduler Comparison**: `docs/SCHEDULER_COMPARISON.md`

---

## Quick Test Examples

### Test Spark Job Classification
```bash
go test -v ./pkg/workload/ -run TestClassifyPod_SparkJob
```

### Test Gang Scheduling Plugin
```bash
go test -v ./pkg/plugins/coscheduling/ -run TestName
```

### Test Resource Reservation
```bash
go test -v ./pkg/plugins/resourcereservation/ -run TestNewResourceReservation
```

### Run All Tests with Coverage
```bash
go test -coverprofile=coverage.out ./pkg/...
go tool cover -func=coverage.out | grep total
```

---

**Last Updated**: January 2025  
**Maintainer**: KubeNexus Scheduler Team
