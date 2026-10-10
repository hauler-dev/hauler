# Consumer API surface — hauler as an in-process library

## Overview

This document is the contract for anyone who links hauler as a **Go library** rather than shelling out to
the `hauler` CLI. It records the final module and package paths on the reconciled `local/hauler-pr-fixes`
branch, the exact signature of every symbol an external consumer calls, and — most importantly — every
place the surface shifted from the pre-rebase branch, with a suggested way to adapt. If you are that
consumer, read the deviations table in [Going further](#going-further) first; one of them
(`AddLocalImage`'s new return value) is the only change likely to stop your code compiling.

## Context

Hauler was written as a CLI, but a downstream codebase wanted to drive it **in-process**: create a content
store, pull images/charts/files into it, pack it into a haul, and load it back — all without spawning a
process or parsing CLI output. That means the functions the CLI calls internally had to become *exported*
and *stable*.

Our fork carried that export work on `local/hauler-pr-fixes`, cut from a stale base. Upstream `main` moved
on substantially in the meantime, so the branch was re-expressed on top of current upstream. Most of the
surface survived untouched; a few symbols changed shape because upstream had already solved the same
problem a different way (and, per the rebase rules, upstream's solution wins unless there's a
consumer-facing reason not to).

Think of this document as the "what you can call, and what changed" sheet for that consumer. It is
generated from the landed code, not from the original request — every signature below was checked against
the source on the branch.

## Getting started

The smallest real thing a consumer does: open a store, pull an image into it, and write the exports
manifest that a later `store save`/`store load` round-trip depends on.

```go
import (
	"context"

	"hauler.dev/go/hauler/v2/pkg/store"
)

func pull(ctx context.Context, dir string) error {
	// 1. Create (or reopen) an OCI layout backed content store.
	layout, err := store.NewLayout(dir)
	if err != nil {
		return err
	}

	// 2. Pull an image into it. Named fields mean new options are additive.
	if err := store.AddImageWithOpts(ctx, layout, "registry.example.com/app:v1",
		store.ImageAddOptions{}); err != nil {
		return err
	}

	// 3. Write the exports manifest for the platform you intend to load on.
	return store.WriteExportsManifest(ctx, dir, "")
}
```

That compiles against the branch as-is. The import path carries the `/v2` segment — that is the single
most common edit when moving from the old branch (see the module-path deviation below).

## How it works

### Module and package paths

- **Module:** `hauler.dev/go/hauler/v2` (`go.mod:1`), `go 1.27.1`. Every import carries the `/v2` segment.
- **Exported surface packages** the consumer touches:

  | Package | What lives there |
  | --- | --- |
  | `pkg/store` | The content store: `NewLayout`, `Add*`, `Sync`, `RewriteReference`, `WriteExportsManifest`, `Open`, `ResolveStoreDir` |
  | `pkg/apis/hauler.cattle.io/v1` | Declarative types: `File`, `Image`, `Chart`, `Git`, `Directory` |
  | `pkg/archives` | `Archive`, `Unarchive`, and the `CompressionMap` / `ArchivalMap` lookups |
  | `pkg/flags` | Flag/option structs: `StoreRootOpts`, `LoadOpts`, `CliRootOpts` |
  | `cmd/hauler/cli/store` | `LoadCmd` — the programmatic entry point for `store load` |
  | `pkg/log` | `Fields`, the zerolog field map |
  | `pkg/consts` | Annotation keys such as `ContainerdImageNameKey` |

  `pkg/flags` was relocated here from `internal/flags`: an `internal/` package is not importable outside
  the module, so the consumer could never have reached it. The relocation also split the package so that
  `pkg/flags` no longer imports `pkg/store` (which would have been an import cycle) — store construction
  now lives in `pkg/store` as `Open` and `ResolveStoreDir`.

### The surface, symbol by symbol

Legend: **unchanged** = identical to the pre-rebase branch; **additive** = same call shape, new fields;
**changed** = the call shape differs.

**`pkg/store`**

| Symbol | Final signature (file:line) | vs pre-rebase |
| --- | --- | --- |
| `NewLayout` | `func NewLayout(rootdir string, opts ...Options) (*Layout, error)` (`store.go:81`) | unchanged |
| `(*Layout).AddLocalImage` | `func (l *Layout) AddLocalImage(ctx context.Context, ref string) (string, error)` (`store.go:416`) | **changed** — was `error` |
| `WriteExportsManifest` | `func WriteExportsManifest(ctx context.Context, dir string, platformStr string) error` (`save.go:119`) | unchanged |
| `AddChartWithOpts` | `func AddChartWithOpts(ctx context.Context, s *Layout, chartRef string, opts ChartAddOptions) error` (`add.go:229`) | unchanged |
| `AddFile` | `func AddFile(ctx context.Context, s *Layout, fi v1.File) error` (`add.go:37`) | unchanged |
| `Sync` | `func Sync(ctx context.Context, s *Layout, opts SyncOptions) error` (`sync.go:223`) | unchanged |
| `AddImageWithOpts` | `func AddImageWithOpts(ctx context.Context, s *Layout, ref string, opts ImageAddOptions) error` (`add.go:85`) | unchanged; `ImageAddOptions` gained `CaFile` |
| `RewriteReference` | `func RewriteReference(ctx context.Context, s *Layout, oldRef name.Reference, newRef name.Reference, rawRewrite string) error` (`add.go:128`) | unchanged |
| `*store.Layout` (type) | `type Layout struct { *content.OCI; Root string; StoreID string; haulerDir string; cache layer.Cache; blobConcurrency int }` (`store.go:42`) | additive — `StoreID` exported |

**`pkg/apis/hauler.cattle.io/v1`**

| Symbol | Final shape (file:line) | vs pre-rebase |
| --- | --- | --- |
| `File` | `type File struct { Path string; Name string; CaFile string; InsecureSkipTLSVerify bool }` (`file.go:18`) | additive — `Path`/`Name` unchanged, two TLS fields added |

**`pkg/archives`**

| Symbol | Final signature (file:line) | vs pre-rebase |
| --- | --- | --- |
| `Archive` | `func Archive(ctx context.Context, dir, outfile string, compression archives.Compression, archival archives.Archival) error` (`archiver.go:42`) | unchanged |
| `CompressionMap` / `ArchivalMap` | `var CompressionMap` (`archiver.go:16`), `var ArchivalMap` (`archiver.go:26`); `"zst"` and `"tar"` present | unchanged |
| `Unarchive` | `func Unarchive(ctx context.Context, tarball, dst string) error` (`unarchiver.go:128`) | unchanged |

**`pkg/flags`**

| Symbol | Final shape (file:line) | vs pre-rebase |
| --- | --- | --- |
| `StoreRootOpts` | `type StoreRootOpts struct { StoreDir string; Retries int; TempOverride string; BlobConcurrency int }` (`store.go:11`) | additive — `BlobConcurrency` added |
| `LoadOpts` | `type LoadOpts struct { *StoreRootOpts; FileName []string }` (`load.go:8`) | unchanged — still embeds `*StoreRootOpts` |
| `CliRootOpts` | `type CliRootOpts struct { LogLevel string; HaulerDir string; IgnoreErrors bool; AuditLevel string; WorkDir string }` (`cli.go:5`) | additive — `AuditLevel`, `WorkDir` added |

**`cmd/hauler/cli/store`**

| Symbol | Final signature (file:line) | vs pre-rebase |
| --- | --- | --- |
| `LoadCmd` | `func LoadCmd(ctx context.Context, o *flags.LoadOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error` (`load.go:32`) | **unchanged signature** (behavior changed — see Gotchas) |

**`pkg/log`** — `type Fields map[string]string` (`log.go:30`), unchanged.

**`pkg/consts`** — `ContainerdImageNameKey = "io.containerd.image.name"`, `KindAnnotationName = "kind"`,
`KindAnnotationImage = "dev.hauler/image"` (`consts.go:50-52`), all unchanged.

### The save/load dance

`WriteExportsManifest` is the piece that ties the library surface to the CLI's `store save --containerd`
behavior, and the consumer relies on its exact shape:

1. Create the `oci-layout` (via `NewLayout`).
2. Call `WriteExportsManifest(ctx, dir, platform)` to emit the manifest.
3. **Strip the marker before packing** — mirroring the CLI. Without the strip, containerd mis-loads the
   archive.
4. After `LoadCmd`, **backfill `oci-layout`** — the loader does not write it.

None of those steps changed in this rebase, but they are the reason the manifest function is part of the
public surface at all.

## Going further

### Deviations from the pre-rebase surface

Every row here is a real difference between the branch before and after the rebase. "Old" is the
pre-rebase shape; "new" is what you compile against now.

| Symbol | Old → new | Reason | Suggested adaptation |
| --- | --- | --- | --- |
| Module path | `hauler.dev/go/hauler` → `hauler.dev/go/hauler/v2` | Adopt upstream `main`'s path | Add `/v2` to every import |
| `internal/flags` import | `…/v2/internal/flags` → `…/v2/pkg/flags` | `internal/` is not importable outside the module | Update the import path |
| `(*Layout).AddLocalImage` | `error` → `(string, error)` | Upstream returns the stored digest for pinned-digest verification | A bare call still compiles; if you assigned the single `error`, switch to `_, err :=` or consume the digest |
| `ImageAddOptions` | + `CaFile string` | Upstream made TLS explicit | Optional field; named literals unaffected |
| `v1.File` | + `CaFile`, `InsecureSkipTLSVerify` | Upstream made TLS explicit | Optional fields; `File{Path, Name}` literals unaffected |
| `flags.StoreRootOpts` | + `BlobConcurrency int` | Upstream blob-write concurrency knob | Optional field |
| `flags.CliRootOpts` | + `AuditLevel`, `WorkDir` | Upstream audit / work-dir flags | Optional fields |
| `store.Layout` | + exported `StoreID string` | Upstream persistent store identity | Optional; positional struct literals break, named ones do not |
| `ResolveStoreDir` | `ResolveStoreDir(root string) (string, error)` → `ResolveStoreDir(ctx context.Context, ro *flags.CliRootOpts, storeDir string) (string, error)` | Store-id resolution + hauler-dir lookup | Pass `ctx` and root opts |
| `Store(ctx, ro)` | method on `*StoreRootOpts` → free function `store.Open(ctx, rso, ro)` | A method can't live on a type in another package, and `pkg/flags` must not import `pkg/store` | Replace `rso.Store(ctx, ro)` with `store.Open(ctx, rso, ro)` |
| `pkg/flags` → `pkg/store` dependency | imported → no longer imports | Breaks the cycle introduced by the relocation | None — `pkg/flags` is now pure data + flag registration |

### What upstream superseded

- **Insecure-registry / TLS.** Our fork's `WithInsecureRegistry(ctx)` *context* approach is gone — the
  type and helpers were deleted. Upstream's **explicit parameters** win: `ImageAddOptions.InsecureSkipTLSVerify`
  and `.CaFile`, the `v1.Image`/`v1.File` TLS fields, and the CLI `--insecure-skip-tls-verify` /
  `--ca-file`. If you injected insecure state through the context, pass it as a parameter instead.
- **`ProcessContent` / `ProcessImageTxt`** exported wrappers are dropped; upstream's `Sync` is the entry
  point. (Only the test names `TestProcessContent_*` remain, exercising the unexported CLI path.)
- **Chart package** moved from `pkg/content/chart` to `pkg/artifacts/chart`; `AddChartWithOpts` uses it.
- **Helm v3 → v4** (`helm.sh/helm/v4 v4.3.0`); `AddChartWithOpts` internals use v4 chart APIs.
- **Chunk naming** changed from `<base>_N<ext>` to `<name>.NNN`; the unambiguous legacy subset is still
  recognized on load.

### Intentionally dropped / left open

- **AppleDouble `._*` blobs** are gone from the tree, and `.gitignore` now carries `._*` so they cannot
  return.
- **Vanity-import gap (left open):** `static/index.html` declares a `go-import` for the un-suffixed
  `hauler.dev/go/hauler`; there is no `static/go/hauler/v2/` stub. `static/` is upstream-owned, so this
  was not touched here.
- **`consts.MemoryConfigMediaType`** was removed pre-rebase in favor of `GitRepoConfigMediaType` (not
  consumer-listed).

### Build and test status

`go build ./...` exits 0 and `go test ./...` passes across every package (including `pkg/store`,
`pkg/archives`, `pkg/flags`, `internal/surface`, and the CLI packages). The `pkg/store` acceptance tests
use an in-memory registry and a fake Docker daemon and passed. **Not exercised in this environment:** real
remote registries and a real Docker daemon.

## Gotchas

- **`AddLocalImage` now returns `(string, error)`.** This is the one deviation most likely to break your
  build. If your call site is `if err := layout.AddLocalImage(ctx, ref); err != nil`, change it to
  `if _, err := layout.AddLocalImage(ctx, ref); err != nil`. The string is the stored digest.
- **Error-string coupling.** The consumer treats go-containerregistry's `"no child with platform"` as a
  benign miss. That text is still what the code produces (pinned by `pkg/store/add_test.go`), but it is a
  string match — a different error shape would become a hard failure for you.
- **Logging comes from context.** Hauler sources its zerolog logger from the context; the consumer injects
  one. Nothing here changed, but be aware a change to that mechanism would silently drop your logging.
- **`LoadCmd`'s signature is unchanged, but its behavior is not.** The pre-rebase branch was already
  4-argument; the loader's internals changed with the rebase. If you depend on side effects (like the
  missing `oci-layout` backfill), re-test your round-trip.
- **`golang.org/x/sync` bumped to `v0.23.0`.** A consumer comment relied on hauler pinning `v0.7.0` (no
  `errgroup.WithoutCancel`). That assumption is stale — re-check the comment.
- **`store.Layout` gained an exported `StoreID`.** Named-field literals are fine; a positional literal
  would break.

## See also

- `pkg/store/store.go`, `pkg/store/add.go`, `pkg/store/save.go`, `pkg/store/sync.go` — the store surface.
- `pkg/flags/store.go`, `pkg/flags/load.go`, `pkg/flags/cli.go` — the option structs.
- `internal/surface/surface_test.go` — compile-time guards that pin several of the symbols above.
- `pkg/store/add_test.go` (`TestNewLayout_RootAndConsumerSignature`) and
  `cmd/hauler/cli/store/load_signature_test.go` — consumer-style signature pins.
- `README.md` and `DEVELOPMENT.md` — CLI usage and build/test instructions.
