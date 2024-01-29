SHELL:=/bin/bash
BUILD_OS=darwin
BUILD_ARCH=arm64 
GO_FILES=$(shell go list ./... | grep -v /vendor/)

COSIGN_VERSION=v2.2.2+carbide.2

BUILD_VERSION=$(shell cat VERSION)
BUILD_TAG=$(BUILD_VERSION)

.SILENT:

all: fmt vet install test

build:
	rm -rf cmd/hauler/binaries;\
	mkdir -p cmd/hauler/binaries;\
    wget -P cmd/hauler/binaries/ https://github.com/rancher-government-carbide/cosign/releases/download/$(COSIGN_VERSION)/cosign-$(BUILD_OS)-$(BUILD_ARCH);\
	mkdir bin;\
	GOENV=GOARCH=$(BUILD_ARCH) CGO_ENABLED=0 go build -o bin ./cmd/...;\

build-all: fmt vet
	goreleaser build --rm-dist --snapshot
	
install:
	rm -rf cmd/hauler/binaries;\
	mkdir -p cmd/hauler/binaries;\
    wget -P cmd/hauler/binaries/ https://github.com/rancher-government-carbide/cosign/releases/download/$(COSIGN_VERSION)/cosign-$(BUILD_OS)-$(BUILD_ARCH);\
	GOENV=GOARCH=$(BUILD_ARCH) CGO_ENABLED=0 go install ./cmd/...;\

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
