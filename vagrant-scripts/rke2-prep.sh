#!/bin/sh

################################################################################
# RUN IN VAGRANT MACHINE
# Download all required dependencies for an air-gapped rke2 install, saving them
# to the folder shared with the host machine.
################################################################################

BASE_SHARED_DIR="/opt/hauler"
VAGRANT_SCRIPTS_DIR="${BASE_SHARED_DIR}/vagrant-scripts"
ARTIFACTS_DIR="${BASE_SHARED_DIR}/local-artifacts/rke2"

RKE2_VERSION='v1.18.4-beta16+rke2'
RKE2_VERSION_URL='v1.18.4-beta16%2Brke2'
RKE2_VERSION_DOCKER='v1.18.4-beta16-rke2'

LOCAL_IMAGES="${ARTIFACTS_DIR}/images"
LOCAL_BIN="${ARTIFACTS_DIR}/bin"
LOCAL_RPM="${ARTIFACTS_DIR}/rpm"

mkdir -p ${LOCAL_IMAGES}
mkdir -p ${LOCAL_BIN}
mkdir -p ${LOCAL_RPM}

# temporarily allow internet access
${VAGRANT_SCRIPTS_DIR}/airgap.sh internet

pushd ${LOCAL_IMAGES}

curl -LO https://github.com/rancher/rke2/releases/download/${RKE2_VERSION_URL}/rke2-images.linux-amd64.tar.gz
gunzip rke2-images.linux-amd64.tar.gz

popd

pushd ${LOCAL_BIN}

curl -L https://github.com/rancher/rke2/releases/download/${RKE2_VERSION_URL}/rke2-installer.linux-amd64.run -o rke2-installer.run
chmod +x ./*

popd

pushd ${LOCAL_RPM}

# TODO - add RPMs

popd


# restore air-gap configuration
${VAGRANT_SCRIPTS_DIR}/airgap.sh airgap
