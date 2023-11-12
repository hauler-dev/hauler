#!/bin/bash

# function to display error and exit
function error_exit {
    echo "Hauler: $1"
    exit 1
}

# check for required tools
command -v curl >/dev/null 2>&1 || error_exit "curl is not installed"
command -v tar >/dev/null 2>&1 || error_exit "tar is not installed"
command -v sha256sum >/dev/null 2>&1 || error_exit "sha256sum is not installed"

# set version or default to latest release
version=${HAULER_VERSION:-0.4.0}

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
        error_exit "Unsupported Platform: $platform"
        ;;
esac

# detect the architecture
arch=$(uname -m)
case $arch in
    x86_64)
        arch="amd64"
        ;;
    aarch64)
        arch="arm64"
        ;;
    *)
        error_exit "Unsupported Architecture: $arch"
        ;;
esac

# display the version, platform, and architecture
echo "Version: $version | Platform: $platform | Architecture: $arch"

# download the checksum file
curl -sOL https://github.com/rancherfederal/hauler/releases/download/v${version}/hauler_${version}_checksums.txt || error_exit "Failed to Download the Checksums File"

# download the tar.gz file
curl -sOL https://github.com/rancherfederal/hauler/releases/download/v${version}/hauler_${version}_${platform}_${arch}.tar.gz || error_exit "Failed to Download the Archive"

# verify the checksum
checksum_match=$(sha256sum -c --ignore-missing hauler_${version}_checksums.txt 2>/dev/null | grep "hauler_${version}_${platform}_${arch}.tar.gz: OK")
if [ -z "$checksum_match" ]; then
    error_exit "Failed Checksum Verification"
fi

# uncompress the archive
tar -xzf "hauler_${version}_${platform}_${arch}.tar.gz" || error_exit "Failed to Extract the Archive"

# install the binary
case "$platform" in
    linux)
        sudo mv "hauler" "/usr/local/bin" || error_exit "Failed to Move Binary to /usr/local/bin"
        ;;
    darwin)
        sudo mv "hauler" "/usr/local/bin" || error_exit "Failed to Move Binary to /usr/local/bin"
        ;;
    *)
        error_exit "Unsupported Platform/Architecture: $platform/$arch"
        ;;
esac

# clean up the files
rm hauler_${version}_checksums.txt hauler_${version}_${platform}_${arch}.tar.gz

# display success message
echo "Installation Successful! Hauler v${version} is now available for use!"