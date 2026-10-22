# Justification of changes — `feature/export-cli-functionality`

## Overview

This document justifies every change the branch `feature/export-cli-functionality` makes relative to
`main`, exactly as captured by `git diff main...feature/export-cli-functionality`. The branch is a rebase of
the fork's library-export work onto current upstream, so the diff is dominated by **package relocations**
rather than new behaviour. We therefore describe the package moves **once**, as a single general change, and
group the remaining changes by purpose so nothing is repeated. Each entry points at the code, test or
document that carries it; anything we could not explain would be listed under "Unexplained — needs review"
rather than given an invented reason.

## Context

Hauler began life as a CLI, but a downstream codebase wants to drive it **in-process** as a Go library:
open a content store, pull images/charts/files into it, pack a haul, and load it back — without spawning a
process or parsing CLI output. That means the functions the CLI calls internally had to become *exported*
and *stable*.

Our fork carried that export work on `local/hauler-pr-fixes`, cut from a stale base. Upstream `main` moved
on substantially in the meantime, so the branch was re-expressed on top of current upstream. Two
consequences shape this document:

- **Most of the diff is mechanical.** `internal/flags` moved to `pkg/flags` so an external module can
  import it (`internal/` is not importable outside the module), and every import that named the old path
  was updated to follow. That churn is one change, not dozens.
- **The genuinely new surface is smaller and purposeful.** The bulk export functions (`AddFile`,
  `AddChartWithOpts`, `Sync`, `WriteExportsManifest`, …) are a deliberate set of additions, grouped below.

**Scope note.** This document covers only `main...feature/export-cli-functionality`. Changes that already
exist on `main` — the helm/containerd/cosign upgrades, audit logging, the live progress renderer,
`store check`, concurrency knobs, git/directory artifacts, and the CI/dependabot edits — are **not** part
of this PR and are not listed here. Likewise, the `/v2` module path is already present on both sides
(`go.mod` has an empty diff), so it is not a change this PR introduces.

## The one general change — package moves & import-path churn

Everything below is described **once**, here. It accounts for the large majority of the changed files.

- **`internal/flags` → `pkg/flags`.** The package moved so an external module can import it (`internal/`
  is not importable outside the module). Git represents the move as **19 pure renames**
  (`{internal => pkg}/flags/*.go`, zero content change): `add.go`, `cli.go`, `concurrency.go`,
  `concurrency_test.go`, `copy.go`, `create.go`, `extract.go`, `ignore_errors.go`,
  `ignore_errors_test.go`, `info.go`, `load.go`, `no_progress_test.go`, `remove.go`, `retries.go`,
  `save.go`, `serve.go`, `serve_test.go`, `sync.go`, `version.go`. Two test files are **deleted and
  re-added trimmed** — `store_blob_concurrency_test.go` and `store_retries_test.go` keep only their
  `AddFlags` registration test under `pkg/flags/` (24 lines each); their `Store()`-behaviour cases live on
  in `pkg/store/flags_test.go` (see the store-core group). `internal/flags/store.go` is a **rename with
  edits** to `pkg/store/flags.go` (the store-construction relocation below). And `pkg/flags/store.go` is a
  **new** file — the split-out `StoreRootOpts` + `AddFlags` + `ResolveWorkDir` — not a same-named move.
  Because git shows the move as renames, the diff stays far smaller than a naive delete+add count would
  suggest.
- **Import-path churn.** Every `hauler.dev/go/hauler/v2/internal/flags` import becomes
  `.../pkg/flags`. Confirmed import-only diffs in `cmd/hauler/main.go`; `cmd/hauler/cli/{cli,completion,
  version,store}.go`; `cmd/hauler/cli/store_info_check_test.go`; most of `cmd/hauler/cli/store/*.go`;
  `internal/server/{file,git,git_test,server_test}.go`; `pkg/audit/{audit,audit_test}.go`;
  `pkg/cosign/{verifier,verifier_test}.go`; and `pkg/retry/{retry,retry_test}.go`. (The only `pkg/store`
  file with an import change is the new `flags.go`; the rest of `pkg/store/*.go` are new store-core files,
  not import churn — see the store-core group.)
- **Store-construction relocation (same general change).** `(*flags.StoreRootOpts).Store(ctx, ro)` became
  the free function `pkg/store.Open(ctx, rso, ro)`, and `flags.ResolveStoreDir` became
  `pkg/store.ResolveStoreDir` (new file `pkg/store/flags.go`). Call sites: `cmd/hauler/cli/store.go`
  (15 `pkgstore.Open` calls) and `cmd/hauler/cli/store/sync.go` (`store.ResolveStoreDir`, `store.Open`).
  **Reason:** a method cannot live on a type in another package, and `pkg/flags` must not import
  `pkg/store` — doing so would create an import cycle.

## Change groups

The remaining, substantive changes are grouped by purpose.

**One entry per file.** The general change above owns the moves and import churn; the purpose groups own
the substantive edits. Three files are named by both by design — `pkg/store/flags.go`,
`cmd/hauler/cli/store.go` and `cmd/hauler/cli/store/sync.go` are relocations the general change names and
substantive edits a purpose group expands. Read "exactly one entry" as "exactly one *owner*", with the
general entry as the fallback for anything a purpose group does not claim.

### Store core — the new `pkg/store` export surface

This is the heart of the library-export work: the functions a downstream Go program calls to build and
pack a content store, all living in **new** files under `pkg/store/`. The group has three parts — the
relocation of store construction out of `pkg/flags`, the bulk export surface itself, and the tests that
carry its contracts.

#### Store construction moves to `pkg/store.Open`

`(*flags.StoreRootOpts).Store(ctx, ro)` is gone from `pkg/flags`; it is now the free function
`store.Open(ctx, rso, ro)` (`pkg/store/flags.go:58`). Its own doc comment states the reason verbatim:

> This is upstream's `(*StoreRootOpts).Store` body as a free function — a method cannot live on a type in
> another package. (`pkg/store/flags.go:54-57`)

A Go method can only be declared on a type in its own package, so once `StoreRootOpts` lives in
`pkg/flags`, the method that builds a store cannot stay attached to it. The alternative — letting
`pkg/flags` import `pkg/store` to keep the method — would create an import cycle, since `pkg/store`
already imports `pkg/flags`. Hence the free function in `pkg/store`, which is free to import `pkg/flags`.
This is the same relocation the general change above names; here we expand it rather than repeat it.

Call sites updated to the new form: `cmd/hauler/cli/store.go` (15 `pkgstore.Open(...)` calls, at lines 57,
144, 200, 223, 246, 270, 308, 341, 397, 425, 459, 514, 575, 622, 664) and
`cmd/hauler/cli/store/{load,sync}.go` (`store.Open`). See also `docs/consumer-api-surface.md:79-82` and its
deviations-table row (`:168`).

#### `ResolveStoreDir` moves to `pkg/store`

`flags.ResolveStoreDir` becomes `store.ResolveStoreDir(ctx, ro, storeDir)` (`pkg/store/flags.go:22`),
split out of the old `Store()` so `sync` can resolve a target store's path *before* deciding to open it:

> split out of Store() so sync can check a target store's path before deciding to open one.
> (`pkg/store/flags.go:20-21`)

Callers: `Open` itself (`flags.go:61`) and `cmd/hauler/cli/store/sync.go:515`. The fork's older two-arg
`ResolveStoreDir(root string)` was deliberately **not** re-added: a second definition would collide with
this one. Only the three-arg form exists.

#### The bulk export surface

Three new files carry the exported API:

| File | Exported surface |
| --- | --- |
| `add.go` (875 lines) | `AddFile`, `AddImageWithOpts`/`ImageAddOptions`, `AddChartWithOpts`/`ChartAddOptions`, `RewriteReference`, `(*Layout).WriteImage`, plus the shared flags-based layer `StoreFile`/`StoreImage`/`StoreImageNoRetry`/`StoreChart`/`RewriteIndexRef`/`ExtractChartImages`/`ExtractLockImages`/`ApplyDefaultRegistry` |
| `save.go` (227 lines) | `Exports`, `WriteExportsManifest` |
| `sync.go` (794 lines) | `SyncOptions`, `Sync`, `processContent`, `processImageTxt` |

Signatures (from `docs/consumer-api-surface.md:89-101`, cross-checked against the tree):

- `AddFile(ctx, s *Layout, fi v1.File) error` — `add.go:37`
- `AddImageWithOpts(ctx, s *Layout, ref string, opts ImageAddOptions) error` — `add.go:85`
- `RewriteReference(ctx, s *Layout, oldRef, newRef name.Reference, rawRewrite string) error` — `add.go:128`
- `AddChartWithOpts(ctx, s *Layout, chartRef string, opts ChartAddOptions) error` — `add.go:229`
- `WriteExportsManifest(ctx, dir string, platformStr string) error` — `save.go:119`
- `Sync(ctx, s *Layout, opts SyncOptions) error` — `sync.go:223`

Three behavioural contracts are the reason this surface exists, and each is pinned by a test:

- **The save/load dance.** `WriteExportsManifest` writes *only* `manifest.json` and neither creates nor
  removes the `oci-layout` marker — the caller owns the marker (`save.go:114-119`). Pinned by
  `TestWriteExportsManifest_DoesNotTouchOCILayoutMarker` (`save_test.go`). Rationale:
  `docs/consumer-api-surface.md:136-148`.
- **Context logger.** Every entry point sources its logger via `log.FromContext(ctx)` (`add.go:86`,
  `save.go:120`), so a caller's `ctx` controls logging.
- **Benign error string.** go-containerregistry's `"no child with platform"` must survive wrapping, so a
  caller can still recognise it; pinned by `TestAddImageWithOpts_PlatformErrorPreserved` (`add_test.go`).

The port is **not** verbatim, because the fork was cut from a stale base and several upstream APIs moved
under it. The mandatory adaptations: `AddImage` arity grew to 7+variadic; `(*Layout).WriteImage` gained a
leading `ctx`; `HelmValues` became `ValuesFiles []string`; `getter.NewHttp(false, "")`; the cosign
`Cache`/`Verifier` API; and `ApplyDefaultRegistry` was split into a wrapper plus `_applyDefaultRegistry`.

One fork feature was **dropped**: `WithInsecureRegistry(ctx)` / `insecureRegistryFromContext` are absent
from `pkg/store` (grep confirms no matches). Upstream replaced the context approach with explicit
`insecureSkipTLSVerify`/`caFile` parameters, and `ImageAddOptions` carries additive
`InsecureSkipTLSVerify bool` and `CaFile string` fields instead.

#### Chart export via upstream's `pkg/artifacts/chart`

`add.go:23` imports `hauler.dev/go/hauler/v2/pkg/artifacts/chart` and calls `chart.NewChart(...)` at
`add.go:591`; `sync.go:737` reaches the same path through `AddChartWithOpts`. Upstream deleted
`pkg/content/chart` and moved it to `pkg/artifacts/chart` (with helm v4), so the fork's
`pkg/content/chart` is **not** re-added — its one fork fix (`} else {` → `} else if opts.RepoURL != "" {`)
is already upstream at `pkg/artifacts/chart/chart.go:123`. The deletion is visible in the diff:
`pkg/content/chart/chart.go` and `pkg/content/chart/chart_test.go` are deleted files. The coverage travels
with it — `pkg/artifacts/chart/` ships `chart.go` plus `auth_test.go`, `chart_test.go`, `path_test.go`,
`tls_test.go`.

#### Tests: what moved, what was added, what was dropped

- **Relocated.** `pkg/store/flags_test.go:13-17` says the `Store()`-behaviour tests moved from `pkg/flags`
  to `pkg/store` when `Store` became `Open`. The fork's `pkg/flags` never carried a `Store()` test file of
  its own, so read "moved from" loosely. The tests themselves pin env resolution, explicit-value
  precedence, negative-value rejection, and the receiver mutation (`rso.StoreDir`/`BlobConcurrency`/
  `Retries`) callers depend on.
- **Added (all new).** `add_test.go` (1403 lines), `sync_test.go` (692), `store_acceptance_test.go` (533),
  `save_test.go` (255), `flags_test.go` (248). `add_test.go`, `save_test.go` and `sync_test.go` come from
  the fork; `store_acceptance_test.go` and `flags_test.go` were added during the rebase.
- **Dropped.** `pkg/content/chart/chart_test.go` went with the deleted package (see above).
- **Adapted, not deleted.** Fork tests exercising the removed context-insecure path were rewritten onto
  the explicit `InsecureSkipTLSVerify`/`CaFile` fields. The insecure-registry test asserts against a
  self-signed TLS registry plus the transport itself, so it fails if the flag stops reaching the client.

**Known coverage caveat.** `TestAddImageWithOpts_InsecureSkipTLSVerify_SelfSigned` skips when no
non-loopback, non-RFC1918 local address exists — the common case in a container — so that acceptance
behaviour may not be exercised in CI.

### CLI support — `LoadCmd` 4-arg + the CLI construction seam

The `cmd/hauler/cli/**` changes are the thin layer that turns the new library surface into cobra commands.
Almost all of it is the general package move (the `internal/flags` → `pkg/flags` import churn) plus one
cosmetic alias; the substantive pieces are the store-construction seam, the `LoadCmd` signature, and the
adoption of upstream's explicit TLS parameters in place of the fork's context-based approach.

#### CLI wiring — the `storecli` alias and the `pkgstore.Open` seam

In `cmd/hauler/cli/store.go` the `cmd/hauler/cli/store` package is now imported as `storecli`
(`store.go:10`) and all 17 references to it were renamed `store.` → `storecli.`; the `pkg/store` import
keeps its `pkgstore` alias (`store.go:14`). The alias is purely cosmetic — it keeps the `store` *command*
package visually distinct from `pkgstore`.

The real change is the construction seam: every `o.Store(ctx, ro)` / `rso.Store(ctx, ro)` call becomes
`pkgstore.Open(ctx, o.StoreRootOpts, ro)` (or `pkgstore.Open(ctx, rso, ro)` in `addStoreRemove`). The diff
shows this at 15 call sites — extract, sync, serve-registry, serve-files, serve-git, save, info, copy,
add-file, add-directory, add-git, add-image, add-chart, create-manifest, remove. This is the same
relocation the general change names: a method cannot live on a type in another package, and `pkg/flags`
must not import `pkg/store` (import cycle), so store construction became the free function `store.Open`
(`docs/consumer-api-surface.md:168`). The fork's own `resolveAndBuild(dir)` helper is **superseded** by
`pkgstore.Open` and is not re-added; no `store.` reference to the command package remains.

#### `LoadCmd` — 4-arg, layout built internally

`cmd/hauler/cli/store/load.go:32` is now:

```go
func LoadCmd(ctx context.Context, o *flags.LoadOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error
```

Upstream's form was 5-arg: it took a pre-built `s *store.Layout`. That parameter is dropped, and the
layout is built *inside* the function via `store.Open(ctx, rso, ro)` (`load.go:35-38`). The
`tempOverride := rso.TempOverride` line stays *after* that call (`load.go:40-41`) because `store.Open` is
what resolves `TempOverride`; its comment was updated from "by Store()" to "by store.Open". The
`addStoreLoad` call site (`store.go:157-167`) drops its own `pkgstore.Open` call and simply invokes
`storecli.LoadCmd(ctx, o, rso, ro)`.

Why keep the 4-arg form rather than upstream's 5-arg one? Because the downstream consumer's
compile-critical surface calls `LoadCmd(ctx, loadOpts, rootOpts, cliOpts)` — 4 arguments. We prefer
upstream's shape everywhere unless there is a consumer-facing reason not to, and this is one. Building the
layout internally also lets `Open` resolve `StoreDir`/`TempOverride` (replacing the fork's
`pkgstore.ResolveStoreDir(o.StoreDir)` call) and yields `s.StoreID`/`s.Root` for the audit entry.
`NewLayout` does not write `oci-layout`, so the loader still never writes the marker.

> **Hazard to remember.** `LoadCmd` resolves the store from `rso` but unarchives into `o.StoreDir`. The
> CLI constructs `o := &flags.LoadOpts{StoreRootOpts: rso}` (`store.go:158`), so the two are the same
> pointer. A caller passing *distinct* pointers must set `StoreDir` on the pointer passed as `rso`.

#### Explicit TLS / insecure-registry parameters supersede the context approach

This item is a **dropped fork change, not an addition**: the fork's context approach is superseded by
upstream's explicit parameters, which this branch adopts. The fork carried
`WithInsecureRegistry(ctx)` / `insecureRegistryFromContext`; grep across the tree returns no matches in Go
code, so it is not re-added. What the branch carries instead is upstream's explicit parameters
`--insecure-skip-tls-verify` and `--ca-file`, registered in `pkg/flags`
(`pkg/flags/add.go:41-42,55-56,88-89,128-129`; `pkg/flags/sync.go:63-64`; `pkg/flags/copy.go:24`) and
consumed in `cmd/hauler/cli/store.go` (env resolution in `addStoreSync`'s `PreRunE`, `store.go:78-94` and
`store.go:501-507`) and throughout `cmd/hauler/cli/store/sync.go` / `add.go` (e.g.
`sync.go:161-162,197,249,603-604`). `docs/consumer-api-surface.md:171-176` records the decision and names
the concrete parameters; the deviations-table rows (`:162-163`) record the additive `ImageAddOptions.CaFile`
and `v1.File{CaFile, InsecureSkipTLSVerify}` fields.

#### Other command-wiring changes

- **`resolveTargetStore` in `cmd/hauler/cli/store/sync.go`** now calls
  `store.ResolveStoreDir(ctx, ro, target)` (`sync.go:515`) and `store.Open(ctx, &altOpts, ro)`
  (`sync.go:531`) instead of `flags.ResolveStoreDir` / `altOpts.Store(ctx, ro)` — the same relocation as
  the seam above.
- **Everything else under `cmd/hauler/cli/**`** is import-path churn (`internal/flags` → `pkg/flags`) or
  the test call-site updates below. **No flag is newly *registered* under `cmd/hauler/cli/**`** in this
  diff: flag registration lives in the relocated `pkg/flags`, and `cmd/hauler/cli/**` only consumes the
  option structs.

#### Tests

- **New:** `cmd/hauler/cli/store/load_signature_test.go` (127 lines) pins the 4-arg signature at compile
  time (`TestLoadCmd_ConsumerSignature`, line 24), the no-pre-built-layout resolve-and-load behaviour, the
  audit entry (`StoreID`/`Store`/`Command`), and the "loader does not write `oci-layout`" contract.
- **Adapted:** `load_test.go` (9 call sites) and `lifecycle_test.go` (6 call sites) drop the pre-built
  layout argument and remove the now-unused pre-load `store.NewLayout(...)` constructions
  (`singlePreLoad`, `doublePreLoad`, `preLoad`, `storeBPreLoad`, the first `s`); post-load layouts
  (`dest`, re-opened `s`/`singleStore`/`doubleStore`/`storeB`) are kept. No assertion was weakened.

**Caveat.** The "opens the store exactly once" property has **no dedicated unit test**; it is a structural
fact of the diff (`addStoreLoad` no longer opens) and is impractical to pin directly. Note also that
`docs/consumer-api-surface.md:129` calls `LoadCmd`'s signature "unchanged": that is relative to the fork,
which was already 4-arg. Relative to upstream `main` (5-arg) it changed, as described above.

### Consumer API surface

This group is the consumer-facing contract itself: the surface a downstream Go program compiles against,
the deviations it must adapt to, and the guard that keeps the surface from drifting silently. Two pieces
carry it — `docs/consumer-api-surface.md` (the consumer contract, with its deviations table) and the new
`internal/surface` guard package. Seven surface items are worth naming; **they are not all the same kind
of change**, and the distinction decides what this section may claim.

**Read this first — the framing trap.** Three of the seven are *not* changes this PR introduces. They are
deviations from the **pre-rebase fork branch**, which is exactly what the deviations table in
`docs/consumer-api-surface.md` compares against ("Old" = pre-rebase shape). The module path, the
`AddLocalImage` return value, and the insecure-registry supersession are already present on both sides of
`main...feature/export-cli-functionality`, so they are not in this PR's diff. The Overview above sets this
precedent for the module path; this section stays consistent with it.

| Item | Kind |
| --- | --- |
| `internal/flags` → `pkg/flags` | **PR change** (files deleted/added in the diff) |
| break the `pkg/flags` → `pkg/store` cycle | **PR change** (part of the relocation) |
| `(*StoreRootOpts).Store` → `pkg/store.Open`; `ResolveStoreDir` relocation | **PR change** (`pkg/store/flags.go` is new) |
| compile-time guard `internal/surface` | **PR change** (both files new) |
| module path `…/hauler` → `…/hauler/v2` | **not a PR change** — `go.mod` diff is empty |
| `AddLocalImage` return-value change | **not a PR change** — `pkg/store/store.go` diff is empty |
| upstream superseding the fork's insecure-registry approach | **not a PR change** — the fork-only code never existed on `main` |

#### The PR changes

**`internal/flags` → `pkg/flags`.** The package moved so an external module can import it; the diff shows
19 pure renames, one rename-with-edits (`internal/flags/store.go` → `pkg/store/flags.go`), two
delete-and-re-add-trimmed test files, and three new files under `pkg/flags/` (the general change above
gives the exact accounting). The reason is stated verbatim in the consumer contract:
"`pkg/flags` was relocated here from `internal/flags`: an `internal/` package is not importable outside the
module, so the consumer could never have reached it" (`docs/consumer-api-surface.md:69-72`). Every
`hauler.dev/go/hauler/v2/internal/flags` import follows to `.../pkg/flags`.

**Breaking the `pkg/flags` → `pkg/store` import cycle.** On `main`, `internal/flags/store.go` imported
`pkg/store` (for `ResolveStoreID`, `MatchesStoreID`, `Layout`, `Options`, `WithHaulerDir`,
`WithBlobConcurrency`, `NewLayout`). Once the package is importable and `pkg/store/flags.go:12` imports
`pkg/flags`, a `pkg/flags` → `pkg/store` import would be a cycle. It is broken: `grep "pkg/store" pkg/flags`
returns no match — `pkg/flags` is now pure data + flag registration. See
`docs/consumer-api-surface.md:79-82` and deviations row `:169`.

**`(*StoreRootOpts).Store` → `pkg/store.Open`; `ResolveStoreDir` relocation.** The deleted
`internal/flags/store.go` carried the method `(*StoreRootOpts).Store(ctx, ro)` and `ResolveStoreDir`; the
new `pkg/store/flags.go` carries both as free functions (`Open` at `:58`, `ResolveStoreDir` at `:22`). The
new code's own doc comment states the reason verbatim: "This is upstream's `(*StoreRootOpts).Store` body as
a free function — a method cannot live on a type in another package" (`pkg/store/flags.go:54-57`).
`ResolveStoreDir` was "split out of Store() so sync can check a target store's path before deciding to open
one" (`pkg/store/flags.go:20-21`). Call sites updated in `cmd/hauler/cli/store.go` and
`cmd/hauler/cli/store/sync.go`. See `docs/consumer-api-surface.md:79-82` and deviations rows `:167-168`.

**Compile-time guard `internal/surface`.** Both files are new: `internal/surface/doc.go` and
`internal/surface/surface_test.go`. The doc comment states the purpose — pin the consumer-facing
compile-time surface so a shape change "fails the build here rather than silently breaking the downstream
consumer" (`internal/surface/doc.go:1-9`). `TestConsumerSurfaceCompiles` pins, by construction,
`v1.File{Path, Name}`, `log.Fields{}`, the three `consts` annotation keys,
`flags.StoreRootOpts{StoreDir, TempOverride}`, `flags.LoadOpts{StoreRootOpts, FileName}`, and
`flags.CliRootOpts{}` (`internal/surface/surface_test.go:12-47`). It deliberately does **not** reference
`pkg/store`'s export surface or `pkg/archives`.

#### The pre-rebase deviations (not introduced by this PR)

These three are **not** `main...feature` changes — they are differences from the pre-rebase fork branch,
recorded in the deviations table of `docs/consumer-api-surface.md`.

**Module path `hauler.dev/go/hauler` → `hauler.dev/go/hauler/v2`.** `go.mod:1` is
`module hauler.dev/go/hauler/v2`; `git diff main...feature -- go.mod` is empty, so both sides already carry
`/v2`. The PR does not adopt the path — the pre-rebase fork did (`docs/consumer-api-surface.md:159`,
deviations row).

**`AddLocalImage` return-value change.** `pkg/store/store.go:416` is
`func (l *Layout) AddLocalImage(ctx context.Context, ref string) (string, error)`, and
`git diff main...feature -- pkg/store/store.go` is empty — the `(string, error)` shape is already on
`main`. It is a deviation from the pre-rebase fork, where it returned `error`
(`docs/consumer-api-surface.md:94` "**changed** — was `error`"; `:161` deviations row). This is the one
compile-breaking deviation: a call site that assigned the single `error` must switch to `_, err :=` or
consume the digest.

**Upstream superseding the fork's insecure-registry approach.** `grep` for
`WithInsecureRegistry|insecureRegistryFromContext` across the tree matches only docs prose — no Go code —
so the fork's context-based approach is absent, and it never existed on `main`. What the branch carries
instead is upstream's explicit parameters: `ImageAddOptions.InsecureSkipTLSVerify`/`CaFile`, the
`v1.Image`/`v1.File` TLS fields, and the CLI `--insecure-skip-tls-verify` / `--ca-file`
(`docs/consumer-api-surface.md:171-176` and deviations rows `:162-163`).

**Not verified here.** The downstream consumer is a separate codebase, so its exact call sites — in
particular whether it assigns `AddLocalImage`'s return to a single `error` — are taken from
`docs/consumer-api-surface.md`, not independently checked.

#### Files

- `docs/consumer-api-surface.md` (new) — the consumer contract: module/package paths, the symbol-by-symbol
  surface, and the deviations table.
- `internal/surface/doc.go` + `internal/surface/surface_test.go` (new) — the compile-time guard described
  above.

### Junk removal

- `.gitignore` — adds a `._*` rule immediately after `**/.DS_Store`, so committed AppleDouble blobs cannot
  return. This is the **only** junk-related change in `main...feature`; the AppleDouble blobs themselves
  (`pkg/content/._oci.go`, `pkg/store/._add.go`, `pkg/store/._store.go`) are not deleted in this diff
  because they are not on `main`.

### Unexplained — needs review

**None.** Every file in `git diff main...feature/export-cli-functionality` is accounted for by exactly one
entry — the general moves/import-churn change or a purpose group. If a later diff adds a file, list it here
rather than inventing a reason for it.

**Coverage tally.** General moves + import churn: 60 files (25 `internal/flags` move entries + 35
import-only diffs). Store core: 8. CLI support: 6. Consumer API surface: 3. Junk removal: 1. Total: 78.

## See also

- [`docs/consumer-api-surface.md`](consumer-api-surface.md) — the consumer-facing surface and deviations
  table this PR must preserve.
