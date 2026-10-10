# Hauler - Airgap Swiss Army Knife

![hauler-logo](/static/hauler-logo.png)

## What is Hauler?

`Hauler` is a free and open source tool that simplifies delivering artifacts into disconnected and airgapped environments without requiring operators to adopt a specific workflow on either side. It represents artifacts, such as images, charts, files, and more, as content and collections, so operators can fetch, store, package, and distribute them with declarative manifests or the command line, whether the destination is disconnected, airgapped, limited, constrained, or anywhere else your artifacts need to go.

`Hauler` does this by storing content and collections as OCI artifacts and serving them through embedded services, such as a registry, fileserver, and more. Every artifact keeps its signatures, attestations, and SBOMs, which can be verified before it is saved and again after it is loaded, so teams on the disconnected and airgapped side know exactly what they received and where it originated.

`Hauler` replaces the custom scripts and ad hoc tooling that disconnected and airgapped delivery usually requires. It is one binary, one archive, and one workflow, from a single file to entire product suites on Linux, macOS, or Windows, so teams spend less time moving software and more time using it.

`Hauler` is proudly developed and maintained by **[Rancher Government](https://github.com/ranchergovernment)!!**

For more information, please review the **[Hauler Documentation](https://docs.hauler.dev)!**

## Installation

### Linux/Darwin

```bash
# installs latest release
curl -sfL https://get.hauler.dev | bash
```

### Homebrew

```bash
# installs latest release
brew tap hauler-dev/homebrew-tap
brew install hauler

# installs specific release
brew tap hauler-dev/homebrew-tap
brew install hauler@2.1.1

# installs latest release, release candidate, or dev build
brew tap hauler-dev/homebrew-tap
brew install hauler-dev
```

### Windows

```powershell
# installs latest release
irm https://get.hauler.dev/install.ps1 | iex
```

## Known Issues and Limits

<!-- known-limits:start -->
Please report any issues to [Hauler](https://github.com/hauler-dev/hauler/issues), [Hauler Helm](https://github.com/hauler-dev/hauler-helm/issues), or [Hauler Docs](https://github.com/hauler-dev/hauler-docs/issues).

### Breaking Changes

Please see the [release notes](https://github.com/hauler-dev/hauler/releases) for full details.

| Release Version | Change |
|:---:|:---:|
| v2.1.1 | `--platform` is rejected with a digest-pinned multi-platform index |
| v2.1.1 | errors retrieving signatures, attestations, SBOMs, and referrers fail the image instead of being skipped |
| v2.1.0 | `hauler store add image` and `hauler store sync` fail when an image fails signature verification, instead of skipping it... with `--ignore-errors` the unverified image is stored with a warning |
| v2.1.0 | `hauler store info -o json` changed shape |
| v2.1.0 | flag precedence is CLI first, then per-item fields, then manifest annotations |
| v2.0.0 | removed `apiVersion: hauler.cattle.io/v1alpha1`... use `hauler.cattle.io/v1` |

### Known Limitations

These are current limitations of Hauler, with workarounds included where available.

| Command or Area | Limitation |
|:---:|:---:|
| `hauler store load` | podman generated tarballs are not supported and may not load every image |
| `hauler store copy` | copying to a registry path requires `hauler login <registry-url>` without the `<path>` first |
| `hauler store add chart` | a chart with the same name as the store may fail to fetch, since helm checks for a local directory with that name first |
| temporary space | defaults to the os temp directory (i.e. `/tmp`), which needs as much free space as the store or haul... or set `--tempdir` / `HAULER_TEMP_DIR` |
| homebrew | versioned casks (`hauler@<version>`) never upgrade... use `hauler` or `hauler-dev` to receive upgrades |

### Experimental Features

Commands and flags marked `(EXPERIMENTAL)` are not yet stable and may change in a future release.

| Release Version | Command or Flag |
|:---:|:---:|
| v2.1.1 | `hauler store copy --type` |
| v2.1.1 | `hauler store serve registry --basic-auth` / `--basic-auth-realm` |
| v2.1.1 | `hauler store serve fileserver --basic-auth` / `--basic-auth-realm` |
| v2.1.0 | `hauler store create manifest` |

<!-- known-limits:end -->

## Acknowledgements

`Hauler` wouldn't be possible without the open-source community, but there are a few projects that stand out:

- [containerd](https://github.com/containerd/containerd)
- [go-containerregistry](https://github.com/google/go-containerregistry)
- [cosign](https://github.com/sigstore/cosign)
