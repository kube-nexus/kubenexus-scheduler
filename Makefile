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
	$(COMMONENVVAR) $(BUILDENVVAR) go build -ldflags '-w' -o bin/$(BINARY_NAME) cmd/scheduler/main.go

.PHONY: build-webhook
build-webhook:
	@echo "Building kubenexus-webhook..."
	$(COMMONENVVAR) $(BUILDENVVAR) go build -ldflags '-w' -o bin/kubenexus-webhook cmd/webhook/main.go

.PHONY: test
test:
	@echo "Running tests..."
	$(BUILDENVVAR) go test -v ./pkg/...

.PHONY: test-webhook
test-webhook:
	@echo "Running webhook tests..."
	$(BUILDENVVAR) go test -v ./pkg/webhook/...

.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	$(BUILDENVVAR) go test -v -timeout 120s ./test/integration/...

.PHONY: test-e2e
test-e2e:
	@echo "E2E tests require a Kind cluster with KWOK and fake GPU nodes."
	@echo "Run hack/e2e-setup.sh first, then: go test -v -timeout 300s ./test/e2e/"
	@echo "Skipping E2E tests in CI (no cluster available)..."

.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(BUILDENVVAR) go test -coverprofile=coverage.out ./pkg/...
	$(BUILDENVVAR) go tool cover -html=coverage.out -o coverage.html

.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	GOOS=linux GOARCH=amd64 $(BUILDENVVAR) go build -ldflags '-w' -o bin/$(BINARY_NAME)-linux cmd/scheduler/main.go
	docker build -t $(DOCKER_IMAGE):$(VERSION) .

.PHONY: dockersimple
dockersimple:
	@echo "Building scheduler with Dockerfile.simple..."
	GOOS=linux GOARCH=amd64 $(BUILDENVVAR) go build -ldflags '-w' -o bin/$(BINARY_NAME)-linux cmd/scheduler/main.go
	cp bin/$(BINARY_NAME)-linux $(BINARY_NAME)
	docker build -t $(DOCKER_IMAGE):latest -f Dockerfile.simple .
	rm -f $(BINARY_NAME)

.PHONY: kind-load
kind-load:
	@echo "Loading scheduler image into kind cluster..."
	kind load docker-image $(DOCKER_IMAGE):latest --name kubenexus-test

.PHONY: quick-deploy
quick-deploy: dockersimple kind-load
	@echo "Restarting scheduler pod..."
	kubectl delete pod -n kubenexus-system -l app=kubenexus-scheduler || true
	@echo "Waiting for scheduler to be ready..."
	@sleep 10
	kubectl get pods -n kubenexus-system

.PHONY: docker-build-webhook
docker-build-webhook:
	@echo "Building webhook Docker image..."
	docker build -t kubenexus-webhook:$(VERSION) -f Dockerfile.webhook .

.PHONY: docker-push-webhook
docker-push-webhook:
	@echo "Pushing webhook Docker image..."
	docker push kubenexus-webhook:$(VERSION)

.PHONY: generate-webhook-certs
generate-webhook-certs:
	@echo "Generating webhook TLS certificates..."
	bash hack/generate-webhook-certs.sh

.PHONY: deploy-webhook
deploy-webhook: generate-webhook-certs
	@echo "Deploying webhook..."
	kubectl apply -f deploy/webhook-configured.yaml

.PHONY: clean
clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -f pkg/apis/scheduling/v1alpha1/zz_generated.deepcopy.go
	rm -f deploy/webhook-configured.yaml

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

.PHONY: e2e-setup
e2e-setup:
	@echo "Setting up e2e cluster with KWOK fake GPU nodes..."
	./hack/e2e-setup.sh

.PHONY: e2e-test
e2e-test:
	@echo "Running e2e tests..."
	go test ./test/e2e/ -v -count=1

.PHONY: e2e-teardown
e2e-teardown:
	@echo "Tearing down e2e cluster..."
	./hack/e2e-setup.sh teardown

.PHONY: e2e
e2e: e2e-setup e2e-test

.PHONY: kind-logs
kind-logs:
	@echo "Showing scheduler logs..."
	kubectl logs -n kube-system -l app=kubenexus-scheduler -f

.PHONY: verify
verify: generate
	@echo "Verifying generated code is up to date..."
	git diff --exit-code pkg/apis/
