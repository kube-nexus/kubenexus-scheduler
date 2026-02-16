COMMONENVVAR=GOOS=$(shell uname -s | tr A-Z a-z) GOARCH=$(subst x86_64,amd64,$(patsubst i%86,386,$(shell uname -m)))
BUILDENVVAR=CGO_ENABLED=0

.PHONY: all
all: build

.PHONY: build
build: autogen
	$(COMMONENVVAR) $(BUILDENVVAR) go build -ldflags '-w' -o bin/kube-scheduler cmd/main.go

.PHONY: update-vendor
update-vendor:
	hack/update-vendor.sh

.PHONY: autogen
autogen: update-vendor
	hack/update-generated-openapi.sh
