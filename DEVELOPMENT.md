# Development Guide

This document covers how to build Hauler locally and how the project's branching strategy works. It's intended for contributors making code changes or maintainers managing releases.

---

## Local Build

### Prerequisites

- **Go** — check `go.mod` for the minimum required version
- **Make**
- **Docker** (optional, for container image builds)
- **Git**

### Clone the Repository

```bash
git clone https://github.com/hauler-dev/hauler.git
cd hauler
```

### Build the Binary

Using Make:

```bash
make build
```

Or directly with Go:

```bash
go build -o hauler ./cmd/hauler
```

The compiled binary will be output to the project root. You can run it directly:

```bash
./hauler version
```

### Run Tests

```bash
make test
```

Or with Go:

```bash
go test ./...
```

### Useful Tips

- The `--store` flag defaults to `./store` in the current working directory during local testing, so running `./hauler store add ...` from the project root is safe and self-contained. Use `rm -rf store` in the working directory to clear.
- Set `--log-level debug` when developing to get verbose output.

---

## Branching Strategy

Hauler uses a **main-first, release branch** model. All development flows through `main`, and release branches are maintained for each minor version to support patching older release lines in parallel.

### Branch Structure

```
main              ← source of truth, all development targets here
release/1.3       ← 1.3.x patch line
release/1.4       ← 1.4.x patch line
```

Release tags (`v1.4.1`, `v1.3.2`, etc.) are always cut from the corresponding `release/X.Y` branch, never directly from `main`.

### Where to Target Your Changes

All pull requests should target `main` by default. Maintainers are responsible for cherry-picking fixes onto release branches as part of the patch release process.

| Change type | Target branch |
|---|---|
| New features | `main` |
| Bug fixes | `main` |
| Security patches | `main` (expedited backport to affected branches) |
| Release-specific fix (see below) | `release/X.Y` directly |

### Creating a New Release Branch

When `main` is ready to ship a new minor version, a release branch is cut:

```bash
git checkout main
git pull origin main
git checkout -b release/1.4
git push origin release/1.4
```

The first release is then tagged from that branch:

```bash
git tag v1.4.0
git push origin v1.4.0
```

Development on `main` immediately continues toward the next minor.

### Backporting a Fix to a Release Branch

When a bug fix merged to `main` also needs to apply to an active release line, cherry-pick the commit onto the release branch and open a PR targeting it:

```bash
git checkout release/1.3
git pull origin release/1.3
git checkout -b backport/fix-description-to-1.3
git cherry-pick <commit-sha>
git push origin backport/fix-description-to-1.3
```

Open a PR targeting `release/1.3` and reference the original PR in the description. If the cherry-pick doesn't apply cleanly, resolve conflicts and note them in the PR.

### Fixes That Only Apply to an Older Release Line

Sometimes a bug exists in an older release but the relevant code has been removed or significantly changed in `main` — making a forward-port unnecessary or nonsensical. In these cases, it's acceptable to open a PR directly against the affected `release/X.Y` branch.

When doing this, the PR description must explain:

- Which versions are affected
- Why the fix does not apply to `main` or newer release lines (e.g., "this code path was removed in 1.4 when X was refactored")

This keeps the history auditable and prevents future contributors from wondering why the fix never made it forward.

### Summary

```
            ┌─────────────────────────────────────────► main (next minor)
            │
            │  cherry-pick / backport PRs
            │  ─────────────────────────►  release/1.4  (v1.4.0, v1.4.1 ...)
            │
            │  ─────────────────────────►  release/1.3  (v1.3.0, v1.3.1 ...)
            │
            │  direct fix (older-only bug)
            │  ─────────────────────────►  release/1.2  (critical fixes only)
```
