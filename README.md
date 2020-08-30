# k3ama - Air-gap Migration Assistant (working name)
```bash
#  _    _____   _    __  __    _
# | | _|___ /  / \  |  \/  |  / \
# | |/ / |_ \ / _ \ | |\/| | / _ \
# |   < ___) / ___ \| |  | |/ ___ \
# |_|\_\____/_/   \_\_|  |_/_/   \_\ k3s- Air-gap Migration Assistant
#
#                ,        ,  _______________________________
#    ,-----------|'------'|  |                             |
#   /.           '-'    |-'  |_____________________________|
#  |/|             |    |
#    |   .________.'----'    _______________________________
#    |  ||        |  ||      |                             |
#    \__|'        \__|'      |_____________________________|
#
# |‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾|
# |________________________________________________________|
#                                                          |
# |‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾|
# |________________________________________________________|

```
WARNING- Work In Progress

## Prerequisites
* CentOS 7
* User with root/sudo privileges
* 

## Installing on an airgap network
1) (Skip if you aren't using SELINUX) Install the `selinux` dependencies. `yum localinstall -y ./artifacts/yum/*`.
2) For some reason, centos doesn't add `/usr/local/bin` to the path. Add it with `echo 'export PATH=${PATH}:/usr/local/bin' >> ~/.bashrc`
3) 



## Charts to include
* Rancher
* Registry
* Minio
* Longhorn
* git-http-backend
* argo

## TODO
* Write the thing
* Include Vagrantfile for testing

### Other possible names
* k3vac
* k3ziplock
* k3wh - k3 wormhole
* k3cia - Comms insensensitive Assistant
* k3diode

## Go CLI

The initial MVP for a k3ama CLI used to streamline the packaging and deploying processes is in the
`cmd/` and `pkg/` folders, along with `go.mod` and `go.sum`. Currently only a `package` subcommand
is supported, which generates a `.tar.gz` archive used in the future `deploy` subcommand.

### Build

To build k3ama, the Go CLI v1.14 or higher is required. See <https://golang.org/dl/> for downloads
and see <https://golang.org/doc/install> for installation instructions.

To build k3ama for your local machine (usually for the `package` step), run the following:

```bash
mkdir bin
go build -o bin ./cmd/...
```

To build k3ama for linux amd64 (required for the `deploy` step in an air-gapped environment), run
the following:

```bash
mkdir bin-linux-amd64
GOOS=linux GOARCH=amd64 go build -o bin-linux-amd64 ./cmd/...
```
