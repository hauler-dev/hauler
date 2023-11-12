#!/bin/bash

# function to display error and exit
function error_exit {
    echo "Hauler - Error: $1"
    exit 1
}

# check for required tools
command -v curl >/dev/null 2>&1 || error_exit "curl is not installed"
command -v tar >/dev/null 2>&1 || error_exit "tar is not installed"
command -v openssl >/dev/null 2>&1 || error_exit "openssl is not installed"

# start hauler installation
echo -e "\n\c" && echo "Hauler: Starting Installation..."

# set version when specified as an environment variable
version=${HAULER_VERSION:-0.4.0}

# set verision when specified as an argument
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
        error_exit "Unsupported Platform: $platform"
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
        error_exit "Unsupported Architecture: $arch"
        ;;
esac

# display the version, platform, and architecture
echo "- Version: $version"
echo "- Platform: $platform"
echo "- Architecture: $arch"

# download the checksum file
curl -sOL https://github.com/rancherfederal/hauler/releases/download/v${version}/hauler_${version}_checksums.txt || error_exit "Failed to Download the Checksums File"

# download the archive file
curl -sOL https://github.com/rancherfederal/hauler/releases/download/v${version}/hauler_${version}_${platform}_${arch}.tar.gz || error_exit "Failed to Download the Archive"

# start hauler checksum verification
echo -e "\n\c" && echo "Hauler: Starting Checksum Verification..."

# verify the hauler checksum
  expected_checksum=$(awk "/hauler_${version}_${platform}_${arch}\.tar\.gz/ {print \$1}" hauler_${version}_checksums.txt)

  if [ -z "$expected_checksum" ]; then
    error_exit "Failed to Find Checksum for hauler_${version}_${platform}_${arch}.tar.gz"
  fi

  determined_checksum=$(openssl sha256 -r "hauler_${version}_${platform}_${arch}.tar.gz" | awk '{print $1}')

  if [ "$determined_checksum" != "$expected_checksum" ]; then
    error_exit "Failed to Verify Checksum - Expected: $expected_checksum - Determined: $determined_checksum"
  fi

# hauler checksum verified
echo "- Successfully Verified Checksum"

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
echo -e "\n\c" && echo "Hauler: Successfully Installed at /usr/local/bin/hauler"

# display availability message
echo "- Hauler v${version} is now available for use!"

# display hauler docs message
echo "- Documentation: https://hauler.dev" && echo -e "\n\c"
