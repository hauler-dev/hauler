# Hauler Roadmap

## \> v0.2.0

- Leverage `referrers` api to robustly link content/collection
- Support signing for all `artifact.OCI` contents
- Support encryption for `artifact.OCI` layers
- Safely embed container runtime for user created `collections` creation and transformation
- Better defaults/configuration/security around for long-lived embedded registry
- Better support multi-platform content
- Better leverage `oras` (`>=0.5.0`) for content relocation
- Store git repos as CAS in OCI format

## v0.2.0 - MVP 2

- Re-focus on cli and framework for oci content fetching and delivery
- Focus on initial key contents
  - Files (local/remote)
  - Charts (local/remote)
  - Images
- Establish framework for `content` and `collections`
- Define initial `content` types (`file`, `chart`, `image`)
- Define initial `collection` types (`thickchart`, `k3s`)
- Define framework for manipulating OCI content (`artifact.OCI`, `artifact.Collection`)

## v0.1.0 - MVP 1

- Install single-node k3s cluster
  - Support tarball and rpm installation methods
  - Target narrow set of known Operating Systems to have OS-specific code if needed
- Serve container images
  - Collect images from image list file
  - Collect images from image archives
  - Deploy docker registry
  - Populate registry with all images
- Serve git repositories 
  - Collect repos
  - Deploy git server (Caddy? NGINX?)
  - Populate git server with repos
- Serve files
  - Collect files from directory, including subdirectories
  - Deploy caddy file server
  - Populate file server with directory contents
  - NOTE: "generic" option - most other use cases can be satisfied by a specially crafted file
    server directory

## v0.0.x

- Install single-node k3s cluster into an Ubuntu machine using the tarball installation method
