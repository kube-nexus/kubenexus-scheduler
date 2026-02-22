COMMONENVVAR=GOOS=$(shell uname -s | tr A-Z a-z) GOARCH=$(subst x86_64,amd64,$(patsubst i%86,386,$(shell uname -m)))
BUILDENVVAR=CGO_ENABLED=0

BINARY_NAME=kubenexus-scheduler
DOCKER_IMAGE=kubenexus-scheduler
VERSION?=v0.1.0

.PHONY: all
all: generate test build

.PHONY: lint
lint:
	@echo "Running linters..."
	@echo "Checking formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Go code is not formatted:"; \
		gofmt -d .; \
		exit 1; \
	fi
	@echo "Running go vet..."
	go vet ./...
	@echo "Running golangci-lint..."
	golangci-lint run --timeout=5m ./...
	@echo "All linting checks passed!"

.PHONY: fmt
fmt:
	@echo "Formatting code..."
	gofmt -w -s .

.PHONY: pre-commit
pre-commit: fmt lint test
	@echo "Pre-commit checks passed!"

.PHONY: generate
generate:
	@echo "Generating code..."
	go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest object:headerFile="hack/boilerplate.go.txt" paths="./pkg/apis/..."

.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	$(COMMONENVVAR) $(BUILDENVVAR) go build -ldflags '-w' -o bin/$(BINARY_NAME) cmd/main.go

.PHONY: test
test:
	@echo "Running tests..."
	$(BUILDENVVAR) go test -v ./pkg/apis/... ./pkg/plugins/coscheduling/... ./pkg/plugins/resourcereservation/... ./pkg/workload/... ./pkg/utils/... ./pkg/scheduler/...

.PHONY: test-integration
test-integration:
	@echo "Integration tests not yet implemented"
	@echo "Skipping integration tests..."

.PHONY: test-e2e
test-e2e:
	@echo "E2E tests not yet implemented"
	@echo "Skipping E2E tests..."

.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(BUILDENVVAR) go test -coverprofile=coverage.out ./pkg/apis/... ./pkg/plugins/coscheduling/... ./pkg/plugins/resourcereservation/... ./pkg/workload/... ./pkg/utils/... ./pkg/scheduler/...
	$(BUILDENVVAR) go tool cover -html=coverage.out -o coverage.html

.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	GOOS=linux GOARCH=amd64 $(BUILDENVVAR) go build -ldflags '-w' -o bin/$(BINARY_NAME)-linux cmd/main.go
	docker build -t $(DOCKER_IMAGE):$(VERSION) .

.PHONY: clean
clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -f pkg/apis/scheduling/v1alpha1/zz_generated.deepcopy.go

.PHONY: kind-setup
kind-setup:
	@echo "Setting up Kind cluster and deploying scheduler..."
	./hack/test-setup.sh

.PHONY: kind-test
kind-test:
	@echo "Running test workloads..."
	./hack/test-workloads.sh

.PHONY: kind-cleanup
kind-cleanup:
	@echo "Cleaning up Kind cluster..."
	kind delete cluster --name kubenexus-test

.PHONY: kind-logs
kind-logs:
	@echo "Showing scheduler logs..."
	kubectl logs -n kube-system -l app=kubenexus-scheduler -f

.PHONY: verify
verify: generate
	@echo "Verifying generated code is up to date..."
	git diff --exit-code pkg/apis/
