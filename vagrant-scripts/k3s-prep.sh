#!/bin/sh

################################################################################
# RUN IN VAGRANT MACHINE
# Download all required dependencies for an air-gapped k3s install, saving them
# to the folder shared with the host machine.
################################################################################

BASE_SHARED_DIR="/opt/k3ama"
VAGRANT_SCRIPTS_DIR="${BASE_SHARED_DIR}/vagrant-scripts"
ARTIFACTS_DIR="${BASE_SHARED_DIR}/local-artifacts/k3s"

K3S_VERSION='v1.18.8+k3s1'
K3S_VERSION_URL='v1.18.8%2Bk3s1'

LOCAL_IMAGES="${ARTIFACTS_DIR}/images"
LOCAL_BIN="${ARTIFACTS_DIR}/bin"
LOCAL_RPM="${ARTIFACTS_DIR}/rpm"

mkdir -p ${LOCAL_IMAGES}
mkdir -p ${LOCAL_BIN}
mkdir -p ${LOCAL_RPM}

# temporarily allow internet access
${VAGRANT_SCRIPTS_DIR}/airgap.sh internet

pushd ${LOCAL_IMAGES}

curl -LO https://github.com/rancher/k3s/releases/download/${K3S_VERSION_URL}/k3s-airgap-images-amd64.tar

popd

pushd ${LOCAL_BIN}

curl -LO https://github.com/rancher/k3s/releases/download/${K3S_VERSION_URL}/k3s
curl -L https://raw.githubusercontent.com/rancher/k3s/${K3S_VERSION_URL}/install.sh -o k3s-install.sh
chmod +x ./*

popd

pushd ${LOCAL_RPM}

curl -LO https://rpm.rancher.io/k3s-selinux-0.1.1-rc1.el7.noarch.rpm
yum install -y yum-utils
yumdownloader --destdir=. --resolve container-selinux selinux-policy-base

popd

# restore air-gap configuration
${VAGRANT_SCRIPTS_DIR}/airgap.sh airgap
