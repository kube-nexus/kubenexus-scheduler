COMMONENVVAR=GOOS=$(shell uname -s | tr A-Z a-z) GOARCH=$(subst x86_64,amd64,$(patsubst i%86,386,$(shell uname -m)))
BUILDENVVAR=CGO_ENABLED=0

BINARY_NAME=kubenexus-scheduler
DOCKER_IMAGE=kubenexus-scheduler
VERSION?=v0.1.0

.PHONY: all
all: generate test build

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

.PHONY: verify
verify: generate
	@echo "Verifying generated code is up to date..."
	git diff --exit-code pkg/apis/
