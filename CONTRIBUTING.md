# Contributing to KubeNexus Scheduler

Thank you for your interest in contributing to KubeNexus! This document provides guidelines and instructions for contributing.

## Code of Conduct

Be respectful, inclusive, and professional in all interactions.

## How to Contribute

### 1. Fork and Clone

```bash
git clone https://github.com/kube-nexus/kubenexus-scheduler.git
cd kubenexus-scheduler
```

### 2. Create a Branch

```bash
git checkout -b feature/your-feature-name
```

### 3. Make Changes

- Follow Go best practices
- Add unit tests for new features
- Update documentation as needed
- Keep commits focused and atomic

### 4. Run Tests

```bash
make test
make build
```

### 5. Submit a Pull Request

- Provide a clear description
- Reference any related issues
- Ensure all tests pass

## Development Setup

### Prerequisites

- Go 1.19+
- Docker
- Kubernetes cluster (kind, minikube, or cloud)
- kubectl

### Build and Test

```bash
# Build
make build

# Run tests
make test

# Build Docker image
make docker-build

# Run locally
./bin/kubenexus-scheduler --v=3
```

## Code Style

- Follow standard Go formatting (`gofmt`)
- Use meaningful variable names
- Add comments for complex logic
- Keep functions focused and small

## Testing Guidelines

- Write unit tests for new features
- Aim for >80% code coverage
- Test edge cases and error conditions
- Use table-driven tests where appropriate

## Documentation

- Update README.md for user-facing changes
- Add inline code comments
- Create examples for new features
- Update architecture docs if needed

## Questions?

Open an issue or discussion on GitHub.

Thank you for contributing! 🚀
