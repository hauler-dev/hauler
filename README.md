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


## Purpose: collect, transfer, and self-host cloud-native artifacts

Kubernetes-focused software usually relies on CLI binaries, container images, helm charts, and more
for installation. Standing up tools for serving these artifacts, collecting the dependencies, and
mirroring them into the self-hosting solutions is usually a manual process with minimal automation. 

Hauler aims to fill this gap by standardizing low-level components of this stack and automating the
collection and transfer of all artifacts.

## Additional Details

- [Roadmap](./ROADMAP.md)
- [Vagrant](./VAGRANT.md)

## Go CLI

The initial MVP for a hauler CLI used to streamline the packaging and deploying processes is in the
`cmd/` and `pkg/` folders, along with `go.mod` and `go.sum`. Currently only a `package` subcommand
is supported, which generates a `.tar.gz` archive used in the future `deploy` subcommand.

### Build

To build hauler, the Go CLI v1.14 or higher is required. See <https://golang.org/dl/> for downloads
and see <https://golang.org/doc/install> for installation instructions.

To build hauler for your local machine (usually for the `package` step), run the following:

```bash
mkdir bin
go build -o bin ./cmd/...
```

To build hauler for linux amd64 (required for the `deploy` step in an air-gapped environment), run
the following:

```bash
mkdir bin-linux-amd64
GOOS=linux GOARCH=amd64 go build -o bin-linux-amd64 ./cmd/...
```
