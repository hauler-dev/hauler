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

# set functions for logging
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

# check for required packages and dependencies
for cmd in echo curl grep sed rm mkdir awk openssl tar install source; do
    if ! command -v "$cmd" &> /dev/null; then
        fatal "$cmd is required to install Hauler"
    fi
done

# set install directory from argument or environment variable
HAULER_INSTALL_DIR=${HAULER_INSTALL_DIR:-/usr/local/bin}

# ensure install directory exists
if [ ! -d "${HAULER_INSTALL_DIR}" ]; then
    mkdir -p "${HAULER_INSTALL_DIR}" || fatal "Failed to Create Install Directory: ${HAULER_INSTALL_DIR}"
fi

# ensure install directory is writable (by user or root privileges)
if [ ! -w "${HAULER_INSTALL_DIR}" ]; then
    if [ "$(id -u)" -ne 0 ]; then
        fatal "Root privileges are required to install Hauler to Directory: ${HAULER_INSTALL_DIR}"
    fi
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
    HAULER_VERSION=$(curl -sI https://github.com/hauler-dev/hauler/releases/latest | grep -i location | sed -e 's#.*tag/v##' -e 's/^[[:space:]]*//g' -e 's/[[:space:]]*$//g')

    # exit if the version could not be detected
    if [ -z "${HAULER_VERSION}" ]; then
        fatal "HAULER_VERSION is unable to be detected and/or retrieved from GitHub. Please set: HAULER_VERSION"
    fi
fi

# detect the operating system
PLATFORM=$(uname -s | tr '[:upper:]' '[:lower:]')
case $PLATFORM in
    linux)
        PLATFORM="linux"
        ;;
    darwin)
        PLATFORM="darwin"
        ;;
    *)
        fatal "Unsupported Platform: $PLATFORM"
        ;;
esac

# detect the architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64 | x86-32 | x64 | x32 | amd64)
        ARCH="amd64"
        ;;
    aarch64 | arm64)
        ARCH="arm64"
        ;;
    *)
        fatal "Unsupported Architecture: $ARCH"
        ;;
esac

# start hauler installation
info "Starting Installation..."

# display the version, platform, and architecture
verbose "- Version: v${HAULER_VERSION}"
verbose "- Platform: $PLATFORM"
verbose "- Architecture: $ARCH"
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
if ! curl -sfOL "https://github.com/hauler-dev/hauler/releases/download/v${HAULER_VERSION}/hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz"; then
    fatal "Failed to Download: hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz"
fi

# start hauler checksum verification
info "Starting Checksum Verification..."

# verify the Hauler checksum
EXPECTED_CHECKSUM=$(awk -v HAULER_VERSION="${HAULER_VERSION}" -v PLATFORM="${PLATFORM}" -v ARCH="${ARCH}" '$2 == "hauler_"HAULER_VERSION"_"PLATFORM"_"ARCH".tar.gz" {print $1}' "hauler_${HAULER_VERSION}_checksums.txt")
DETERMINED_CHECKSUM=$(openssl dgst -sha256 "hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz" | awk '{print $2}')

if [ -z "${EXPECTED_CHECKSUM}" ]; then
    fatal "Failed to Locate Checksum: hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz"
elif [ "${DETERMINED_CHECKSUM}" = "${EXPECTED_CHECKSUM}" ]; then
    verbose "- Expected Checksum: ${EXPECTED_CHECKSUM}"
    verbose "- Determined Checksum: ${DETERMINED_CHECKSUM}"
    verbose "- Successfully Verified Checksum: hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz"
else
    verbose "- Expected: ${EXPECTED_CHECKSUM}"
    verbose "- Determined: ${DETERMINED_CHECKSUM}"
    fatal "Failed Checksum Verification: hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz"
fi

# uncompress the hauler archive
tar -xzf "hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz" || fatal "Failed to Extract: hauler_${HAULER_VERSION}_${PLATFORM}_${ARCH}.tar.gz"

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
