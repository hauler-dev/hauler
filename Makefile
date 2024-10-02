# Makefile for hauler

# set shell
SHELL=/bin/bash

# set go variables
GO_FILES=./...
GO_COVERPROFILE=coverage.out

# set build variables
BIN_DIRECTORY=bin
DIST_DIRECTORY=dist
BINARIES_DIRECTORY=cmd/hauler/binaries

# builds hauler for current platform
# references other targets
build:
	goreleaser build --clean --snapshot --parallelism 1 --timeout 240m --single-target

# builds hauler for all platforms
# references other targets
build-all:
	goreleaser build --clean --snapshot --parallelism 1 --timeout 240m

# install depedencies
install:
	rm -rf $(BINARIES_DIRECTORY)
	mkdir -p $(BINARIES_DIRECTORY)
	touch cmd/hauler/binaries/file
	go mod tidy
	go mod download
	CGO_ENABLED=0 go install ./cmd/...

# format go code
fmt:
	go fmt $(GO_FILES)

# vet go code
vet:
	go vet $(GO_FILES)

# test go code
test:
	go test $(GO_FILES) -cover -race -covermode=atomic -coverprofile=$(GO_COVERPROFILE)

# cleanup artifacts
clean:
	rm -rf $(BIN_DIRECTORY) $(BINARIES_DIRECTORY) $(DIST_DIRECTORY) $(GO_COVERPROFILE)
