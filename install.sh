#!/bin/bash

# Usage:
#   - curl -sfL... | ENV_VAR=... bash
#   - ENV_VAR=... bash ./install.sh
#   - ./install.sh ENV_VAR=...

# Example:
#   Install Latest Release
#     - curl -sfL https://get.hauler.dev | bash
#   Install Specific Release
#     - curl -sfL https://get.hauler.dev | HAULER_VERSION=0.4.2 bash

# Documentation:
#   - https://hauler.dev
#   - https://github.com/rancherfederal/hauler

# set functions for debugging/logging
function info {
    echo && echo "[INFO] Hauler: $1"
}

function verbose {
    echo "$1"
}

function warn {
    echo && echo "[WARN] Hauler: $1"
}

function fatal {
    echo && echo "[ERROR] Hauler: $1"
    exit 1
}

# check for required dependencies
for cmd in curl sed awk openssl tar rm; do
    if ! command -v "$cmd" &> /dev/null; then
        fatal "$cmd is not installed"
    fi
done

# start hauler installation
info "Starting Installation..."

# set version with an environment variable
version=${HAULER_VERSION:-$(curl -s https://api.github.com/repos/rancherfederal/hauler/releases/latest | grep '"tag_name":' | sed 's/.*"v\([^"]*\)".*/\1/')}

# set verision with an argument
while [[ $# -gt 0 ]]; do
  case "$1" in
    HAULER_VERSION=*)
      version="${1#*=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

# detect the operating system
platform=$(uname -s | tr '[:upper:]' '[:lower:]')
case $platform in
    linux)
        platform="linux"
        ;;
    darwin)
        platform="darwin"
        ;;
    *)
        fatal "Unsupported Platform: $platform"
        ;;
esac

# detect the architecture
arch=$(uname -m)
case $arch in
    x86_64 | x86-32 | x64 | x32 | amd64)
        arch="amd64"
        ;;
    aarch64 | arm64)
        arch="arm64"
        ;;
    *)
        fatal "Unsupported Architecture: $arch"
        ;;
esac

# display the version, platform, and architecture
verbose "- Version: v$version"
verbose "- Platform: $platform"
verbose "- Architecture: $arch"

# download the checksum file
if ! curl -sOL "https://github.com/rancherfederal/hauler/releases/download/v${version}/hauler_${version}_checksums.txt"; then
    fatal "Failed to Download: hauler_${version}_checksums.txt"
fi

# download the archive file
if ! curl -sOL "https://github.com/rancherfederal/hauler/releases/download/v${version}/hauler_${version}_${platform}_${arch}.tar.gz"; then
    fatal "Failed to Download: hauler_${version}_${platform}_${arch}.tar.gz"
fi

# start hauler checksum verification
info "Starting Checksum Verification..."

# Verify the Hauler checksum
expected_checksum=$(awk -v version="$version" -v platform="$platform" -v arch="$arch" '$2 == "hauler_"version"_"platform"_"arch".tar.gz" {print $1}' "hauler_${version}_checksums.txt")
determined_checksum=$(openssl dgst -sha256 "hauler_${version}_${platform}_${arch}.tar.gz" | awk '{print $2}')

if [ -z "$expected_checksum" ]; then
    fatal "Failed to Locate Checksum: hauler_${version}_${platform}_${arch}.tar.gz"
elif [ "$determined_checksum" = "$expected_checksum" ]; then
    verbose "- Expected Checksum: $expected_checksum"
    verbose "- Determined Checksum: $determined_checksum"
    verbose "- Successfully Verified Checksum: hauler_${version}_${platform}_${arch}.tar.gz"
else
    verbose "- Expected: $expected_checksum"
    verbose "- Determined: $determined_checksum"
    fatal "Failed Checksum Verification: hauler_${version}_${platform}_${arch}.tar.gz"
fi

# uncompress the archive
tar -xzf "hauler_${version}_${platform}_${arch}.tar.gz" || fatal "Failed to Extract: hauler_${version}_${platform}_${arch}.tar.gz"

# install the binary
case "$platform" in
    linux)
        install hauler /usr/local/bin || fatal "Failed to Install Hauler to /usr/local/bin"
        ;;
    darwin)
        install hauler /usr/local/bin || fatal "Failed to Install Hauler to /usr/local/bin"
        ;;
    *)
        fatal "Unsupported Platform or Architecture: $platform/$arch"
        ;;
esac

# clean up checksum(s)
rm -rf "hauler_${version}_checksums.txt" || warn "Failed to Remove: hauler_${version}_checksums.txt"

# clean up archive file(s)
rm -rf "hauler_${version}_${platform}_${arch}.tar.gz" || warn "Failed to Remove: hauler_${version}_${platform}_${arch}.tar.gz"

# display success message
info "Successfully Installed at /usr/local/bin/hauler"

# display availability message
verbose "- Hauler v${version} is now available for use!"

# display hauler docs message
verbose "- Documentation: https://hauler.dev" && echo
