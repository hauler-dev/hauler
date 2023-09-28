# Hauler: Airgap Swiss Army Knife

> ⚠️ This project is still in active development and _not_ GA.  While a lot of the core features are ready, we're still adding a _ton_, and we may break API compatibility as we change features in newer versions.

`hauler` simplifies the airgap experience without forcing you to adopt a specific workflow for your infrastructure or application.  

To accomplish this, it focuses strictly on two of the biggest airgap pain points:

* content collection
* content distribution

As OCI registries have become ubiquitous nowadays for storing and distributing containers.  Their success and widespread adoption has led many projects to expand beyond containers.

`hauler` capitalizes on this, and leverages the [`oci`](https://github.com/opencontainers) spec to be a simple, zero dependency tool to collect, transport, and distribute your artifacts.

## Getting started

See the [quickstart](docs/walkthrough.md#Quickstart) for a quick way to get started with some of `haulers` capabilities.

For a guided example of all of `haulers` capabilities, check out the [guided example](docs/walkthrough.md#guided-examples).

## Acknowledgements

`hauler` wouldn't be possible without the open source community, but there are a few dependent projects that stand out:

* [go-containerregistry](https://github.com/google/go-containerregistry)
* [oras](https://github.com/oras-project/oras)
* [cosign](https://github.com/sigstore/cosign)

## Notices

*** WARNING: Deprecated Command *** 

The `hauler download (dl)` and `hauler serve (not the store version)` commands are deprecated and will be removed in a future release of Hauler.