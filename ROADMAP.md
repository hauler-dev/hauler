# Hauler Roadmap

## v0.1.0

- install single-node k3s cluster
- define image list
   - collect images
   - deploy to docker registry
- git repos
   - collect repos
   - deploy caddy git server
- define file list
   - collect files
   - deploy caddy file server
   - NOTE: "generic" option, as most other use cases can be satisfied by a specially crafted file
   server directory


## Potential future features

- Helm charts
   - Pull charts, migrate chart artifacts
   - Analyze images needed, add to dependency list
- Yum repo
   - Target package list, collect all dependencies
   - Deploy fully configured yum repo into file server
- Deploy Minio for S3 API
  - MVP: backed by HA storage solution (e.g. AWS S3, Azure Blob Storage)
  - Stable: backed by local storage, including backups
