# Hauler - Kubernetes Air Gap Migration
```bash
#  _                 _           
# | |__   __ _ _   _| | ___ _ __ 
# | '_ \ / _` | | | | |/ _ \ '__|
# | | | | (_| | |_| | |  __/ |   
# |_| |_|\__,_|\__,_|_|\___|_| 
#
#                ,        ,  _______________________________
#    ,-----------|'------'|  |                             |
#   /.           '-'    |-'  |_____________________________|
#  |/|             |    |
#    |   .________.'----'    _______________________________
#    |  ||        |  ||      |                             |
#    \__|'        \__|'      |_____________________________|
#
# __________________________________________________________
# |                                                        |
# |________________________________________________________|
#
# __________________________________________________________
# |                                                        |
# |________________________________________________________|

```

## WARNING - Work In Progress

API stability (including as a code library and as a network endpoint) is NOT guaranteed before `v1` API definitions and a 1.0 release. The following recommendations are made regarding usage patterns of hauler:
- `alpha` (`v1alpha1`, `v1alpha2`, ...) API versions: use **_only_** through `haulerctl`
- `beta` (`v1beta1`, `v1beta2`, ...) API versions: use as an **_experimental_** library and/or API endpoint
- `stable` (`v1`, `v2`, ...) API versions: use as stable CLI tool, library, and/or API endpoint

## Purpose: collect, transfer, and self-host cloud-native artifacts

Kubernetes-focused software usually relies on executables, archives, container images, helm charts, and more for installation. Collecting the dependencies, standing up tools for serving these artifacts, and mirroring them into the self-hosting solutions is usually a manual process with minimal automation. 

Hauler aims to fill this gap by standardizing low-level components of this stack and automating the collection and transfer of artifacts.

## Additional Details

- [Roadmap](./ROADMAP.md)
- [Vagrant](./VAGRANT.md)

## Go CLI

The initial MVP for a hauler CLI used to streamline the packaging and deploying processes is in the `cmd/` and `pkg/` folders, along with `go.mod` and `go.sum`. Currently only a `package` subcommand is supported, which generates a `.tar.gz` archive used in the future `deploy` subcommand.

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
