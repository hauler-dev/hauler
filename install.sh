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
#     - curl -sfL https://get.hauler.dev | HAULER_INSTALL_DIR=/custom/path bash
#     - HAULER_INSTALL_DIR=/custom/path ./install.sh
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

# exit if unable to run with root privledges
if [ "$(id -u)" -ne 0 ]; then
    fatal "Root privledges are required to install Hauler"
fi

# check for required dependencies
for cmd in echo curl grep sed rm mkdir awk openssl tar; do
    if ! command -v "$cmd" &> /dev/null; then
        fatal "$cmd is not installed"
    fi
done

# set version environment variable
if [ -z "${HAULER_VERSION}" ]; then
    version="${HAULER_VERSION:-$(curl -s https://api.github.com/repos/hauler-dev/hauler/releases/latest | grep '"tag_name":' | sed 's/.*"v\([^"]*\)".*/\1/')}"
else
    version="${HAULER_VERSION}"
fi

# set install directory environment variable
HAULER_INSTALL_DIR=${HAULER_INSTALL_DIR:-/usr/local/bin}

# set uninstall environment variable from argument or environment
if [ "${HAULER_UNINSTALL}" = "true" ]; then
    # remove the hauler binary
    rm -f "${HAULER_INSTALL_DIR}/hauler" || fatal "Failed to Remove Hauler from ${HAULER_INSTALL_DIR}"

    # remove the installation directory
    rm -rf "$HOME/.hauler" || fatal "Failed to Remove Directory: $HOME/.hauler"

    info "Hauler Uninstalled Successfully"
    exit 0
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
verbose "- Version: v$version"
verbose "- Platform: $platform"
verbose "- Architecture: $arch"

# check if install directory exists, create it if not
if [ ! -d "$HOME/.hauler" ]; then
    mkdir -p "$HOME/.hauler" || fatal "Failed to Create Directory: ~/.hauler"
fi

# change to install directory
cd "$HOME/.hauler" || fatal "Failed to Change Directory: ~/.hauler"

# download the checksum file
if ! curl -sfOL "https://github.com/hauler-dev/hauler/releases/download/v${version}/hauler_${version}_checksums.txt"; then
    fatal "Failed to Download: hauler_${version}_checksums.txt"
fi

# download the archive file
if ! curl -sfOL "https://github.com/hauler-dev/hauler/releases/download/v${version}/hauler_${version}_${platform}_${arch}.tar.gz"; then
    fatal "Failed to Download: hauler_${version}_${platform}_${arch}.tar.gz"
fi

# start hauler checksum verification
info "Starting Checksum Verification..."

# verify the Hauler checksum
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
install -m 755 hauler "${HAULER_INSTALL_DIR}" || fatal "Failed to Install Hauler to ${HAULER_INSTALL_DIR}"

# add hauler to the path if necessary
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
        echo "Failed to add ${HAULER_INSTALL_DIR} to PATH: Unsupported Shell"
    fi
fi

# display success message
info "Successfully Installed at ${HAULER_INSTALL_DIR}/hauler"

# display availability message
verbose "- Hauler v${version} is now available for use!"

# display hauler docs message
verbose "- Documentation: https://hauler.dev" && echo
