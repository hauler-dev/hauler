SHELL=/bin/bash

GO_FILES=$(shell go list ./... | grep -v /vendor/)
GO_COVERPROFILE=coverage.out

COSIGN_VERSION=v2.2.3+carbide.2

BIN_DIRECTORY=bin
DIST_DIRECTORY=dist
BINARIES_DIRECTORY=cmd/hauler/binaries

build: install fmt vet test
	mkdir -p $(BIN_DIRECTORY)
	CGO_ENABLED=0 go build -o $(BIN_DIRECTORY) ./cmd/...
	rm -rf $(BINARIES_DIRECTORY)

build-all: install fmt vet test
	mkdir -p $(DIST_DIRECTORY)
	goreleaser build --clean --snapshot
	rm -rf $(BINARIES_DIRECTORY)

install:
	rm -rf $(BINARIES_DIRECTORY)
	mkdir -p $(BINARIES_DIRECTORY)
	wget -q --show-progress -P $(BINARIES_DIRECTORY) https://github.com/hauler-dev/cosign/releases/download/$(COSIGN_VERSION)/cosign-$(shell go env GOOS)-$(shell go env GOARCH)
	CGO_ENABLED=0 go install ./cmd/...

fmt:
	go fmt $(GO_FILES)

vet:
	go vet $(GO_FILES)

test:
	go test $(GO_FILES) -cover -race -covermode=atomic -coverprofile=$(GO_COVERPROFILE)

clean:
	rm -rf $(BIN_DIRECTORY) $(BINARIES_DIRECTORY) $(DIST_DIRECTORY) $(GO_COVERPROFILE)
