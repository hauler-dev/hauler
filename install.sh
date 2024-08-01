#!/bin/bash

# Usage:
#   - curl -sfL... | ENV_VAR=... bash
#   - ENV_VAR=... ./install.sh
#
# Install Usage:
#   Install Latest Release
#     - curl -sfL https://get.hauler.dev | bash
#     - ./install.sh
#
#   Install Specific Release
#     - curl -sfL https://get.hauler.dev | HAULER_VERSION=1.0.0 bash
#     - HAULER_VERSION=1.0.0 ./install.sh
#
#   Set Install Directory
#     - curl -sfL https://get.hauler.dev | HAULER_INSTALL_DIR=/usr/local/bin bash
#     - HAULER_INSTALL_DIR=/usr/local/bin ./install.sh
#
# Debug Usage:
#   - curl -sfL https://get.hauler.dev | HAULER_DEBUG=true bash
#   - HAULER_DEBUG=true ./install.sh
#
# Uninstall Usage:
#   - curl -sfL https://get.hauler.dev | HAULER_UNINSTALL=true bash
#   - HAULER_UNINSTALL=true ./install.sh
#
# Documentation:
#   - https://hauler.dev
#   - https://github.com/hauler-dev/hauler

# set functions for debugging/logging
function verbose {
    echo "$1"
}

function info {
    echo && echo "[INFO] Hauler: $1"
}

function warn {
    echo && echo "[WARN] Hauler: $1"
}

function fatal {
    echo && echo "[ERROR] Hauler: $1"
    exit 1
}

# debug hauler from argument or environment variable
if [ "${HAULER_DEBUG}" = "true" ]; then
    set -x
fi

# start hauler preflight checks
info "Starting Preflight Checks..."

# check for required root privileges
if [ "$(id -u)" -ne 0 ]; then
    fatal "Root privileges are required to install Hauler"
fi

# check for required packages and dependencies
for cmd in echo curl grep sed rm mkdir awk openssl tar; do
    if ! command -v "$cmd" &> /dev/null; then
        fatal "$cmd is required to install Hauler"
    fi
done

# set install directory from argument or environment variable
HAULER_INSTALL_DIR=${HAULER_INSTALL_DIR:-/usr/local/bin}

# ensure install directory exists and writable
if [ ! -d "${HAULER_INSTALL_DIR}" ]; then
    mkdir -p "${HAULER_INSTALL_DIR}" || fatal "Failed to Create Install Directory: ${HAULER_INSTALL_DIR}"
fi

if [ ! -w "${HAULER_INSTALL_DIR}" ]; then
    fatal "Installation Directory is not Writable: ${HAULER_INSTALL_DIR}"
fi

# uninstall hauler from argument or environment variable
if [ "${HAULER_UNINSTALL}" = "true" ]; then
    # remove the hauler binary
    rm -rf "${HAULER_INSTALL_DIR}/hauler" || fatal "Failed to Remove Hauler from ${HAULER_INSTALL_DIR}"

    # remove the working directory
    rm -rf "$HOME/.hauler" || fatal "Failed to Remove Hauler Directory: $HOME/.hauler"

    info "Successfully Uninstalled Hauler" && echo
    exit 0
fi

# set version environment variable
if [ -z "${HAULER_VERSION}" ]; then
    # attempt to retrieve the latest version from GitHub
    HAULER_VERSION=$(curl -s https://api.github.com/repos/hauler-dev/hauler/releases/latest | grep '"tag_name":' | sed 's/.*"v\([^"]*\)".*/\1/')

    # exit if the version could not be detected
    if [ -z "${HAULER_VERSION}" ]; then
        fatal "HAULER_VERSION is unable to be detected and/or retrieved from GitHub"
    fi
fi

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

# start hauler installation
info "Starting Installation..."

# display the version, platform, and architecture
verbose "- Version: v${HAULER_VERSION}"
verbose "- Platform: $platform"
verbose "- Architecture: $arch"
verbose "- Install Directory: ${HAULER_INSTALL_DIR}"

# check working directory and/or create it
if [ ! -d "$HOME/.hauler" ]; then
    mkdir -p "$HOME/.hauler" || fatal "Failed to Create Directory: $HOME/.hauler"
fi

# update permissions of working directory
chmod -R 777 "$HOME/.hauler" || fatal "Failed to Update Permissions of Directory: $HOME/.hauler"

# change to working directory
cd "$HOME/.hauler" || fatal "Failed to Change Directory: $HOME/.hauler"

# start hauler artifacts download
info "Starting Download..."

# download the checksum file
if ! curl -sfOL "https://github.com/hauler-dev/hauler/releases/download/v${HAULER_VERSION}/hauler_${HAULER_VERSION}_checksums.txt"; then
    fatal "Failed to Download: hauler_${HAULER_VERSION}_checksums.txt"
fi

# download the archive file
if ! curl -sfOL "https://github.com/hauler-dev/hauler/releases/download/v${HAULER_VERSION}/hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz"; then
    fatal "Failed to Download: hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz"
fi

# start hauler checksum verification
info "Starting Checksum Verification..."

# verify the Hauler checksum
expected_checksum=$(awk -v version="${HAULER_VERSION}" -v platform="${platform}" -v arch="${arch}" '$2 == "hauler_"version"_"platform"_"arch".tar.gz" {print $1}' "hauler_${HAULER_VERSION}_checksums.txt")
determined_checksum=$(openssl dgst -sha256 "hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz" | awk '{print $2}')

if [ -z "${expected_checksum}" ]; then
    fatal "Failed to Locate Checksum: hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz"
elif [ "${determined_checksum}" = "${expected_checksum}" ]; then
    verbose "- Expected Checksum: ${expected_checksum}"
    verbose "- Determined Checksum: ${determined_checksum}"
    verbose "- Successfully Verified Checksum: hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz"
else
    verbose "- Expected: ${expected_checksum}"
    verbose "- Determined: ${determined_checksum}"
    fatal "Failed Checksum Verification: hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz"
fi

# uncompress the hauler archive
tar -xzf "hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz" || fatal "Failed to Extract: hauler_${HAULER_VERSION}_${platform}_${arch}.tar.gz"

# install the hauler binary
install -m 755 hauler "${HAULER_INSTALL_DIR}" || fatal "Failed to Install Hauler: ${HAULER_INSTALL_DIR}"

# add hauler to the path
if [[ ":$PATH:" != *":${HAULER_INSTALL_DIR}:"* ]]; then
    if [ -f "$HOME/.bashrc" ]; then
        echo "export PATH=\$PATH:${HAULER_INSTALL_DIR}" >> "$HOME/.bashrc"
        source "$HOME/.bashrc"
    elif [ -f "$HOME/.bash_profile" ]; then
        echo "export PATH=\$PATH:${HAULER_INSTALL_DIR}" >> "$HOME/.bash_profile"
        source "$HOME/.bash_profile"
    elif [ -f "$HOME/.zshrc" ]; then
        echo "export PATH=\$PATH:${HAULER_INSTALL_DIR}" >> "$HOME/.zshrc"
        source "$HOME/.zshrc"
    elif [ -f "$HOME/.profile" ]; then
        echo "export PATH=\$PATH:${HAULER_INSTALL_DIR}" >> "$HOME/.profile"
        source "$HOME/.profile"
    else
        warn "Failed to add ${HAULER_INSTALL_DIR} to PATH: Unsupported Shell"
    fi
fi

# display success message
info "Successfully Installed Hauler at ${HAULER_INSTALL_DIR}/hauler"

# display availability message
info "Hauler v${HAULER_VERSION} is now available for use!"

# display hauler docs message
verbose "- Documentation: https://hauler.dev" && echo
