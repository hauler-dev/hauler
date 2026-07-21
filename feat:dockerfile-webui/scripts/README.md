# Maintenance Scripts

This directory contains development and maintenance scripts for Hauler UI.

## Scripts

### cleanup.sh
Removes development artifacts and backup files from the repository.

**Usage:**
```bash
./scripts/cleanup.sh
```

**What it removes:**
- Backup files (`*_original.*`)
- Downloaded binaries
- Redundant documentation

### obfuscate.sh
Manually obfuscates the frontend JavaScript code.

**Usage:**
```bash
./scripts/obfuscate.sh
```

**Note:** This is automatically done during Docker build. Only use for testing obfuscation locally.

### qa-dependencies.sh
Validates that all required dependencies are present in the Docker image.

**Usage:**
```bash
./scripts/qa-dependencies.sh
```

**Tests:**
- Dockerfile dependencies (openssl, curl, bash, ca-certificates)
- Go module dependencies
- Docker build success
- Hauler CLI installation
- Runtime dependencies
- Application binaries

## Moving to Tests

The `qa-dependencies.sh` script could be moved to `/tests/` directory as it's a validation test rather than a maintenance script.
