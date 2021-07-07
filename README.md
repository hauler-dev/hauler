# Hauler - Kubernetes Air Gap Migration

__⚠️ WARNING: This is an experimental, work in progress project.  _Everything_ is subject to change, and it is actively in development, so let us know what you think!__

Hauler is built to be a one stop shop for simplifying the burden of working with kubernetes in airgapped environments.  Utility is split into a few commands intended to assist with increasingly complex airgapped use cases.

__Portable self contained clusters__:

Within the `hauler package` subset of commands, `Packages` (name to be finalized) can be created, updated, and ran.

A `Package` is a hauler specific, configurable, self-contained, compressed archive (`*.tar.zst`) that contains all dependencies needed to 1) create a kubernetes cluster, 2) deploy resources into the cluster.

> **_For more detailed examples,_** take a look at [the examples doc](./EXAMPLES.md).

```bash
# Build a minimal portable k8s cluster
hauler package build

# Build a package that deploys resources when deployed
hauler package build -p path/to/chart -p path/to/manifests -i extra/image:latest -i busybox:musl

# Build a package that deploys a cluster, oci registry, and sample app on boot
# Note the aliases introduced
hauler pkg b -p testdata/docker-registry -p testdata/rawmanifests
```

Hauler packages at their core stand on the shoulders of other technologies (`k3s`, `rke2`, and `fleet`), and as such, are designed to be extremely flexible.

Common use cases are to build turn key, appliance like clusters designed to boot on disconnected or low powered devices.  Or portable "utility" clusters that can act as a stepping stone for further downstream deployable infrastructure.  Since ever `Package` is built as an entirely self contained archive, disconnected environments are _always_ a first class citizen.

__Image Relocation__:

For disconnected workloads that don't require a cluster to be created first, images can be efficiently packaged and relocated with `hauler relocate`.

Images are stored as a compressed archive of an `oci` layout, ensuring only the required de-duplicated image layers are packaged and transferred.

## Installation

Hauler is and will always be a statically compiled binary, we strongly believe in a zero dependency tool is key to reducing operational complexity in airgap environments.

Before GA, hauler can be downloaded from the releases page for every tagged release

## Dev

A `Vagrant` file is provided as a testing ground.  The boot scripts at `vagrant-scripts/*.sh` will be ran on boot to ensure the dev environment is airgapped.

```bash
vagrant up

vagrant ssh
```

More info can be found in the [vagrant docs](VAGRANT.md).

## WIP Warnings

API stability (including as a code library and as a network endpoint) is NOT guaranteed before `v1` API definitions and a 1.0 release. The following recommendations are made regarding usage patterns of hauler:
- `alpha` (`v1alpha1`, `v1alpha2`, ...) API versions: use **_only_** through `haulerctl`
- `beta` (`v1beta1`, `v1beta2`, ...) API versions: use as an **_experimental_** library and/or API endpoint
- `stable` (`v1`, `v2`, ...) API versions: use as stable CLI tool, library, and/or API endpoint

### Build

```bash
# Current arch build
make build

# Multiarch dev build
goreleaser build --rm-dist --snapshot
```
