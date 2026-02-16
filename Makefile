COMMONENVVAR=GOOS=$(shell uname -s | tr A-Z a-z) GOARCH=$(subst x86_64,amd64,$(patsubst i%86,386,$(shell uname -m)))
BUILDENVVAR=CGO_ENABLED=0

BINARY_NAME=kubenexus-scheduler
DOCKER_IMAGE=kubenexus-scheduler
VERSION?=v0.1.0

.PHONY: all
all: test build

.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	$(COMMONENVVAR) $(BUILDENVVAR) go build -ldflags '-w' -o bin/$(BINARY_NAME) cmd/main.go

.PHONY: test
test:
	@echo "Running tests..."
	go test -v -race ./pkg/...

.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	GOOS=linux GOARCH=amd64 $(BUILDENVVAR) go build -ldflags '-w' -o bin/$(BINARY_NAME)-linux cmd/main.go
	docker build -t $(DOCKER_IMAGE):$(VERSION) .

.PHONY: clean
clean:
	rm -rf bin/
