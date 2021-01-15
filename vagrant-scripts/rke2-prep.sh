#!/bin/sh

################################################################################
# RUN IN VAGRANT MACHINE
# Download all required dependencies for an air-gapped rke2 install, saving them
# to the folder shared with the host machine.
################################################################################

BASE_SHARED_DIR="/opt/hauler"
VAGRANT_SCRIPTS_DIR="${BASE_SHARED_DIR}/vagrant-scripts"
ARTIFACTS_DIR="${BASE_SHARED_DIR}/local-artifacts/rke2"

RKE2_VERSION='v1.18.13+rke2r1'
RKE2_VERSION_URL='v1.18.13%2Brke2r1'
RKE2_VERSION_DOCKER='v1.18.13-rke2r1'

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

#pushd ${LOCAL_BIN}
#
#curl -L https://github.com/rancher/rke2/releases/download/${RKE2_VERSION_URL}/rke2-installer.linux-amd64.run -o rke2-installer.run
#chmod +x ./*
#
#popd

pushd ${LOCAL_RPM}

yum install -y yum-plugin-downloadonly

rke2_rpm_channel='stable'
rpm_site='rpm.rancher.io'
maj_ver='7'
rke2_majmin=$(echo "${RKE2_VERSION}" | sed -E -e "s/^v([0-9]+\.[0-9]+).*/\1/")

cat <<-EOF >"/etc/yum.repos.d/rancher-rke2.repo"
[rancher-rke2-common-${rke2_rpm_channel}]
name=Rancher RKE2 Common (${RKE2_VERSION})
baseurl=https://${rpm_site}/rke2/${rke2_rpm_channel}/common/centos/${maj_ver}/noarch
enabled=1
gpgcheck=1
gpgkey=https://${rpm_site}/public.key
[rancher-rke2-${rke2_majmin}-${rke2_rpm_channel}]
name=Rancher RKE2 ${rke2_majmin} (${RKE2_VERSION})
baseurl=https://${rpm_site}/rke2/${rke2_rpm_channel}/${rke2_majmin}/centos/${maj_ver}/x86_64
enabled=1
gpgcheck=1
gpgkey=https://${rpm_site}/public.key
EOF

yum install --downloadonly --downloaddir=./ rke2-server

popd


# restore air-gap configuration
${VAGRANT_SCRIPTS_DIR}/airgap.sh airgap
