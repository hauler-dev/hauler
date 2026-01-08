# Rancher Government Hauler

![rancher-government-hauler-logo](/static/rgs-hauler-logo.png)

## Airgap Swiss Army Knife

`Rancher Government Hauler` simplifies the airgap experience without requiring operators to adopt a specific workflow. **Hauler** simplifies the airgapping process, by representing assets (images, charts, files, etc...) as content and collections to allow operators to easily fetch, store, package, and distribute these assets with declarative manifests or through the command line.

`Hauler` does this by storing contents and collections as OCI Artifacts and allows operators to serve contents and collections with an embedded registry and fileserver. Additionally, `Hauler` has the ability to store and inspect various non-image OCI Artifacts.

For more information, please review the **[Hauler Documentation](https://hauler.dev)!**

## Recent Changes

### In Hauler v1.4.0...

- Added a notice to `hauler store sync --products/--product-registry` to warn users the default registry will be updated in a future release.
  - Users will see logging notices when using the `--products/--product-registry` such as...
  - `!!! WARNING !!! [--products] will be updating its default registry in a future release...`
  - `!!! WARNING !!! [--product-registry] will be updating its default registry in a future release...`

### In Hauler v1.2.0...

- Upgraded the `apiVersion` to `v1` from `v1alpha1`
  - Users are able to use `v1` and `v1alpha1`, but `v1alpha1` is now deprecated and will be removed in a future release. We will update the community when we fully deprecate and remove the functionality of `v1alpha1`
  - Users will see logging notices when using the old `apiVersion` such as...
  - `!!! DEPRECATION WARNING !!! apiVersion [v1alpha1] will be removed in a future release...`
---
- Updated the behavior of `hauler store load` to default to loading a `haul` with the name of `haul.tar.zst` and requires the flag of `--filename/-f` to load a `haul` with a different name
- Users can load multiple `hauls` by specifying multiple flags of `--filename/-f`
  - updated command usage: `hauler store load --filename hauling-hauls.tar.zst`
  - previous command usage (do not use): `hauler store load hauling-hauls.tar.zst`
---
- Updated the behavior of `hauler store sync` to default to syncing a `manifest` with the name of `hauler-manifest.yaml` and requires the flag of `--filename/-f` to sync a `manifest` with a different name
- Users can sync multiple `manifests` by specifying multiple flags of `--filename/-f`
  - updated command usage: `hauler store sync --filename hauling-hauls-manifest.yaml`
  - previous command usage (do not use): `hauler store sync --files hauling-hauls-manifest.yaml`
---
Please review the documentation for any additional [Known Limits, Issues, and Notices](https://docs.hauler.dev/docs/known-limits)!

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
```

### Windows

```bash
# coming soon
```

## Acknowledgements

`Hauler` wouldn't be possible without the open-source community, but there are a few projects that stand out:

- [oras cli](https://github.com/oras-project/oras)
- [cosign](https://github.com/sigstore/cosign)
- [go-containerregistry](https://github.com/google/go-containerregistry)
