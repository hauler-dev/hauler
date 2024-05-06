SHELL:=/bin/bash
GO_FILES=$(shell go list ./... | grep -v /vendor/)

COSIGN_VERSION=v2.2.3+carbide.2

.SILENT:

all: fmt vet install test

build:
	rm -rf cmd/hauler/binaries;\
	mkdir -p cmd/hauler/binaries;\
    curl -Lo cmd/hauler/binaries/cosign-$(shell go env GOOS)-$(shell go env GOARCH) https://github.com/rancher-government-carbide/cosign/releases/download/$(COSIGN_VERSION)/cosign-$(shell go env GOOS)-$(shell go env GOARCH);\
	mkdir -p bin;\
	CGO_ENABLED=0 go build -o bin ./cmd/...;\

build-all: fmt vet
	goreleaser build --rm-dist --snapshot
	
install:
	rm -rf cmd/hauler/binaries;\
	mkdir -p cmd/hauler/binaries;\
	curl -Lo cmd/hauler/binaries/cosign-$(shell go env GOOS)-$(shell go env GOARCH) https://github.com/rancher-government-carbide/cosign/releases/download/$(COSIGN_VERSION)/cosign-$(shell go env GOOS)-$(shell go env GOARCH);\
	CGO_ENABLED=0 go install ./cmd/...;\

vet:
	go vet $(GO_FILES)

fmt:
	go fmt $(GO_FILES)

test:
	go test $(GO_FILES) -cover

integration_test:
	go test -tags=integration $(GO_FILES)

clean:
	rm -rf bin 2> /dev/null
