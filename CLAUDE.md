# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Hauler ("Airgap Swiss Army Knife") is a Go CLI tool by Rancher Government for collecting, packaging, and distributing Kubernetes artifacts (container images, Helm charts, files) for airgapped environments. The module path is `hauler.dev/go/hauler`. There is also a companion web UI (in `backend/` and `frontend/`) that wraps the CLI via HTTP/WebSocket APIs in a Docker container.

## Build & Development Commands

### CLI (main Go project)
```bash
# Build the hauler binary
go build -o hauler ./cmd/hauler

# Run tests (all packages)
go test ./...

# Run a single package's tests
go test ./pkg/store/...

# Run a specific test
go test ./pkg/store/... -run TestLayout_AddOCI

# Format code
gofmt -w .
```

### Web UI (Docker-based)
```bash
# Build and run
docker compose up -d

# Rebuild
docker compose build

# Logs
docker compose logs -f

# Stop and clean volumes
docker compose down -v
```

### Makefile targets
`make build`, `make run`, `make stop`, `make clean`, `make logs`, `make restart`, `make shell`

## Architecture

### CLI Structure (`cmd/hauler/`)
- Entry point: `cmd/hauler/main.go` — creates context, logger, and invokes `cli.New()`
- Command tree built with **cobra**: `cli.go` → `store.go` → `store/*.go`
- Login/logout commands delegate to `go-containerregistry`'s crane auth commands
- All store subcommands: `add {image,chart,file}`, `sync`, `save`, `load`, `copy`, `serve {registry,fileserver}`, `extract`, `info`, `remove`

### Core Packages

**`pkg/store`** — The content store. `Layout` wraps an OCI layout on disk. Key methods: `AddOCI()`, `AddOCICollection()`, `Copy()`, `CopyAll()`, `Flush()`, `RemoveArtifact()`, `CleanUp()` (garbage collection). Store location defaults to `./store` or `$HAULER_STORE_DIR`.

**`pkg/artifacts`** — Defines the `OCI` interface (MediaType, Manifest, RawConfig, Layers) and `OCICollection` interface. Implementations in subdirectories:
- `artifacts/image` — container images via go-containerregistry
- `artifacts/file` — arbitrary files (local, HTTP)
- `artifacts/memory` — in-memory artifacts

**`pkg/content`** — `OCI` type wraps an oras OCI store; `content.go` has `Load()` which parses manifest YAML documents to determine API version/kind. Chart content handling in `content/chart/`.

**`pkg/collection`** — Collections that produce multiple OCI artifacts:
- `collection/chart` — ThickCharts (chart + embedded images)
- `collection/imagetxt` — Image lists from text files (e.g., RKE2 image lists)

**`pkg/apis/hauler.cattle.io/`** — Kubernetes-style API types for manifest YAML. Two API groups:
- `content.hauler.cattle.io` (kinds: Images, Charts, Files, ImageTxts)
- `collection.hauler.cattle.io` (kind: ThickCharts)
- Both `v1` and `v1alpha1` versions exist; v1alpha1 is deprecated and auto-converted

**`pkg/consts`** — All constants: media types (OCI, Docker, Helm, Hauler-specific), annotation keys, env vars, defaults.

**`pkg/cosign`** — Signature verification (key-based and keyless via Sigstore).

**`pkg/reference`** — Image reference parsing and relocation.

**`internal/flags`** — Flag definitions for all CLI commands. `StoreRootOpts` manages store initialization. Each command has its own opts struct.

**`internal/server`** — Server interface + implementations for OCI registry and fileserver serving modes.

**`internal/mapper`** — Maps OCI descriptors to filenames during extract (images get `manifest.json`/`config.json`, charts get `chart.tar.gz`).

### Sync Flow (the central operation)
`store sync -f manifest.yaml` parses multi-document YAML, dispatches by kind:
- **Images** → `storeImage()` with optional cosign verification, platform filtering, rewrite
- **Charts** → `storeChart()` with Helm options
- **Files** → `storeFile()`
- **ThickCharts** → chart + all referenced images as `OCICollection`
- **ImageTxts** → parse text file for image references

### Web UI Architecture
- `backend/main.go` — Go HTTP server (Gorilla Mux, 37 REST endpoints + WebSocket) that shells out to the hauler binary
- `frontend/` — Vanilla JS SPA with Tailwind CSS; obfuscated in Docker build
- `mcp_server/` — Python MCP server for AI assistant integration

### Key Environment Variables
- `HAULER_STORE_DIR` — store directory path
- `HAULER_DIR` — hauler home directory
- `HAULER_TEMP_DIR` — temporary directory override
- `HAULER_IGNORE_ERRORS` — skip errors during operations

### Replace Directives in go.mod
The module uses custom forks: `sigstore/cosign` → `hauler-dev/cosign`, plus pinned versions of `distribution/distribution`, `olekukonko/tablewriter`, and `docker/cli`.

## Testing

Tests are standard Go tests using the `testing` package. Test files exist in `pkg/store/`, `pkg/artifacts/{file,image,memory}/`, `pkg/collection/imagetxt/`, `pkg/content/chart/`, `pkg/getter/`, and `pkg/reference/`. The store test uses temp directories and mock artifacts via `go-containerregistry/pkg/v1/random`.

For the web UI, shell-based test suites exist in `tests/` (`comprehensive_test_suite.sh`, `security_scan.sh`).

## Conventions

- Commit messages follow Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`)
- Go formatting: `gofmt`
- The `boringcrypto.go` file in `cmd/hauler/` enables BoringCrypto for FIPS compliance via build tag
