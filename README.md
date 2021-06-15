# Hauler - Kubernetes Air Gap Migration

## WARNING - Work In Progress

API stability (including as a code library and as a network endpoint) is NOT guaranteed before `v1` API definitions and a 1.0 release. The following recommendations are made regarding usage patterns of hauler:
- `alpha` (`v1alpha1`, `v1alpha2`, ...) API versions: use **_only_** through `haulerctl`
- `beta` (`v1beta1`, `v1beta2`, ...) API versions: use as an **_experimental_** library and/or API endpoint
- `stable` (`v1`, `v2`, ...) API versions: use as stable CLI tool, library, and/or API endpoint

## Purpose: collect, transfer, and self-host cloud-native artifacts

Kubernetes-focused software usually relies on executables, archives, container images, helm charts, and more for installation. Collecting the dependencies, standing up tools for serving these artifacts, and mirroring them into the self-hosting solutions is usually a manual process with minimal automation. 

Hauler aims to fill this gap by standardizing low-level components of this stack and automating the collection and transfer of artifacts.

## Usage

Package a self contained deployable cluster

```bash
# CLI 
# bare k3s cluster
hauler create

# k3s cluster with autodeployed manifests on boot
hauler create -p path/to/rawmanifests -p path/to/kustomizebase -p path/to/helmchart -i image

# Config File
hauler create [-c ./package.yaml]
```

Bootstrap a cluster from a packaged archive

```bash
hauler boot package.tar.zst
```

Relocate a set of images

```bash
hauler save -p path/to/manifests -i image:tag -i image@sha256:...

hauler relocate bundle.tar.zst airgap-registry:5000
```

## Additional Details

- [Roadmap](./ROADMAP.md)
- [Vagrant](./VAGRANT.md)

### Build

To build hauler, the Go CLI v1.14 or higher is required. See <https://golang.org/dl/> for downloads and see <https://golang.org/doc/install> for installation instructions.

To build hauler for your local machine (usually for the `package` step), run the following:

```shell
mkdir bin
go build -o bin ./cmd/...
```

To build hauler for linux amd64 (required for the `deploy` step in an air-gapped environment), run the following:

```shell
mkdir bin-linux-amd64
GOOS=linux GOARCH=amd64 go build -o bin-linux-amd64 ./cmd/...
```
