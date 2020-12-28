# Hauler Roadmap

## v0.1.0

- Install single-node k3s cluster
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


## Potential future features

- Helm charts
  - Pull charts, migrate chart artifacts
  - Analyze required container images, add to dependency list
- Yum repo
  - Provide package list, collect all dependencies
  - Deploy fully configured yum repo into file server
- Deploy Minio for S3 API
  - MVP: backed by HA storage solution (e.g. AWS S3, Azure Blob Storage)
  - Stable: backed by local storage, including backups
- Split archives into chunks of chosen size
  - Enables easier transfer via physical media
  - Allows smaller network transfers, losing less progress on failed upload (or working around timeouts)
