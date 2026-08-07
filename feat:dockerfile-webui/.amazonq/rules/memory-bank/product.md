# Product Overview

## Project Purpose

Hauler UI is a comprehensive web-based interface for Rancher Government's Hauler CLI tool, designed to simplify airgap Kubernetes content management through an intuitive graphical interface. It provides 100% feature parity with the Hauler CLI (all 72 command flags) while making complex operations accessible to users who prefer visual interfaces over command-line tools.

## Value Proposition

- **Complete CLI Coverage**: All 72 Hauler CLI flags implemented across 37 REST API endpoints
- **Airgap Ready**: All assets bundled with no external dependencies, perfect for disconnected environments
- **Visual Content Management**: Interactive browsing and selection of Helm charts and container images
- **Real-time Feedback**: Live log streaming via WebSocket for command execution visibility
- **Single Container Deployment**: Docker-native with persistent storage and health checks
- **Security Focused**: JavaScript obfuscation, secure defaults, and ongoing security hardening

## Key Features

### Store Management
- Add content (Helm charts, container images, files) with full option support
- Sync store from manifests with platform selection and signature verification
- Save/load hauls with compression (tar.zst format)
- Clear store or remove individual artifacts
- Real-time store statistics and content listing

### Repository Management
- Add/remove Helm chart repositories
- Interactive chart browser with version selection
- Batch operations for adding multiple charts simultaneously
- Repository search across configured sources

### Registry Operations
- Configure and manage registry credentials
- Push store contents to private registries
- Registry authentication (login/logout)
- Connection testing and verification

### Advanced Capabilities
- **Signature Verification**: Cosign key upload and verification for supply chain security
- **Multi-Architecture Support**: Platform selection (amd64, arm64, arm/v7)
- **Path Rewriting**: Customize registry paths during content addition
- **TLS Support**: Upload certificates for secure registry and fileserver connections
- **Serve Mode**: Built-in registry and fileserver with TLS support
- **Live Logs**: Real-time command output streaming

## Target Users

### Primary Users
- **DevOps Engineers**: Managing airgap Kubernetes deployments
- **Platform Engineers**: Building and maintaining disconnected infrastructure
- **Security Teams**: Operating in secure, isolated environments
- **System Administrators**: Simplifying Hauler operations without CLI expertise

### Use Cases

1. **Airgap Kubernetes Deployment**
   - Browse and select required Helm charts
   - Add container images for applications
   - Create compressed haul files for transfer
   - Load hauls in disconnected environments

2. **Private Registry Management**
   - Sync content from public registries
   - Push to private/internal registries
   - Manage multi-architecture images
   - Verify signatures for security compliance

3. **Content Curation**
   - Build custom artifact collections
   - Version-specific chart selection
   - Platform-specific image variants
   - Manifest-based synchronization

4. **Development Workflows**
   - Test registry configurations
   - Validate manifest files
   - Extract artifacts for inspection
   - Serve content locally for testing

## Technology Foundation

- **Backend**: Go 1.21 with Gorilla Mux and WebSocket
- **Frontend**: Vanilla JavaScript (obfuscated), Tailwind CSS, Font Awesome
- **Infrastructure**: Docker multi-stage builds, Alpine Linux base
- **Integration**: Direct Hauler CLI binary execution

## Development Methodology

Built using Agentic Prompt Engineering with multi-agent collaboration:
- Product Manager Agent (requirements analysis)
- Software Development Manager Agent (architecture and planning)
- Senior Developer Agents (implementation)
- QA Agent (testing and validation)
- Security Agent (vulnerability assessment)
- Technical Writer Agent (documentation)

## Current Status

**Version**: 3.3.5  
**Status**: Production Ready (security hardening in progress)  
**License**: Apache 2.0  
**Roadmap**: v3.4.0 (Security Hardened), v3.5.0 (Enhanced Features), v4.0.0 (Enterprise Ready)
