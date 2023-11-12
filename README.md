# Rancher Government Hauler

## Airgap Swiss Army Knife

> ⚠️ This project is still in active development and *not* Generally Available (GA). Most of the core functionality and features are ready, but may have breaking changes. Please review the [Release Notes](https://github.com/rancherfederal/hauler/releases) for more information!

`Rancher Government Hauler` simplifies the airgap experience without requiring users to adopt a specific workflow. **Hauler** simplifies the airgapping process, by representing assets (images, charts, files, etc...) as content and collections to allow users to easily fetch, store, package, and distribute these assets with declarative manifests or through the command line.

`Hauler` does this by storing contents and collections as OCI Artifacts and allows users to serve contents and collections with an embedded registry and fileserver. Additionally, `Hauler` has the ability to store and inspect various non-image OCI Artifacts.

For more information, please review the **[Hauler Documentation](https://rancherfederal.github.io/hauler-docs)!**

## Installation

### Linux/Darwin Install
```bash
# install latest version
curl -sfL https://hauler.dev/install.sh | sh

# install a specific version
curl -sfL https://hauler.dev/install.sh | HAULER_VERSION=0.4.0 sh
```

### Windows Install
```bash
# coming soon
```

## Acknowledgements

`Hauler` wouldn't be possible without the open-source community, but there are a few projects that stand out:
* [go-containerregistry](https://github.com/google/go-containerregistry)
* [oras cli](https://github.com/oras-project/oras)
* [cosign](https://github.com/sigstore/cosign)

## Notices
**WARNING - Upcoming Deprecated Command(s):**

`hauler download` (alternatively, `dl`) and `hauler serve` (_not_ `hauler store serve`) commands are deprecated and will be removed in a future release.
