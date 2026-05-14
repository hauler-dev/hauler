# Development Guide

This document covers how to build `hauler` locally and how the project's branching strategy works.

It's intended for contributors making code changes or maintainers managing releases.

---

## Local Build

### Prerequisites

- **Git** - version control of the repository
- **Go** — check `go.mod` for the minimum required version
- **Make** - optional... for common commands used for builds
- **Docker** - optional... for container image builds

### Clone the Repository

```bash
git clone https://github.com/hauler-dev/hauler.git
cd hauler
```

### Build the Binary

Using `make`...

```bash
# run this command from the project root
make build

# the compiled binary will be output to a directory structure and you can run it directly...
./dist/hauler_linux_amd64_v1/hauler
./dist/hauler_linux_arm64_v8.0/hauler
./dist/hauler_darwin_amd64_v1/hauler
./dist/hauler_darwin_arm64_v8.0/hauler
./dist/hauler_windows_amd64_v1/hauler.exe
./dist/hauler_windows_arm64_v8.0/hauler.exe
```

Using `go`...

```bash
# run this command from the project root
go build -o hauler ./cmd/hauler

# the compiled binary will be output to the project root and you can run it directly...
./hauler version
```

### Run Tests

Using `make`...

```bash
make test
```

Using `go`...

```bash
go test ./...
```

### Useful Tips

- The `--store` flag defaults to `./store` in the current working directory during local testing, so running `./hauler store add ...` from the project root is safe and self-contained. Use `rm -rf store` in the working directory to clear.
- Set `--log-level debug` when developing to get verbose output.

---

## Branching Strategy

Hauler uses a **main-first, release branch** model. All development flows through `main` and `release/x.x` branches are maintained for each minor version to support patching older release lines in parallel.

### Branch Structure

```
main          ← source of truth, all development targets here
release/1.3   ← 1.3.x patch line
release/1.4   ← 1.4.x patch line
```

Release tags (`v1.4.1`, `v1.3.2`, etc.) are always cut from the corresponding `release/X.Y` branch, never directly from `main`.

### Where to Target Your Changes

All pull requests should target `main` by default and maintainers are responsible for backporting fixes onto release branches as part of the patch release process.

| Change Type | Target branch |
| :---------: | :-----------: |
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

Backports are handled by [Mergify](https://mergify.com/). When a PR merged to `main` also needs to apply to an active release line, comment on the merged PR with the target branch:

```
@mergifyio backport release/1.3
```

To backport to multiple branches at once, list each target branch in the same comment:

```
@mergifyio backport release/1.3 release/1.4
```

Mergify will open a backport PR against the specified branch with the cherry-picked commits and a reference back to the original PR. The bot adds a label and links the backport PR in the original for traceability.

You can trigger backports either before or after the original PR is merged — Mergify will queue the request and open the backport PR once the original is merged.

#### When the Backport Has Conflicts

If the cherry-pick doesn't apply cleanly, Mergify will still open the backport PR but mark it as having conflicts. To resolve:

```bash
git fetch origin
git checkout mergify/bp/release/1.3/pr-<number>
# resolve conflicts
git add .
git commit
git push
```

Note the conflict resolution in the PR description so reviewers can verify the fix still behaves correctly on the older release line.

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
            │  @mergifyio backport release/1.4
            │  ─────────────────────────►  release/1.4  (v1.4.0, v1.4.1 ...)
            │
            │  @mergifyio backport release/1.3
            │  ─────────────────────────►  release/1.3  (v1.3.0, v1.3.1 ...)
            │
            │  direct fix (older-only bug)
            │  ─────────────────────────►  release/1.2  (critical fixes only)
```
