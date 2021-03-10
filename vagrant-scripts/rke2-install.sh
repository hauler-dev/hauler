#!/bin/sh

################################################################################
# RUN IN VAGRANT MACHINE
# Install a default, bare rke2 cluster into the Vagrant machine
################################################################################

BASE_SHARED_DIR="/opt/hauler"
VAGRANT_SCRIPTS_DIR="${BASE_SHARED_DIR}/vagrant-scripts"

RKE2_VERSION_DOCKER='v1.18.4-beta16-rke2'

if pgrep -x "firewalld" >/dev/null
then
  echo "[FATAL] disable firewalld first"
fi

mkdir -p /etc/rancher/rke2/

# TODO - allow using selinux

SELINUXSTATUS="$(getenforce)"
if [ "$SELINUXSTATUS" = "Permissive" ] || [ "$SELINUXSTATUS" = "Enforcing" ]
then
  echo "selinux: true" | sudo tee -a /etc/rancher/rke2/config.yaml > /dev/null
else
  echo "SELINUX disabled. continuing"
fi

LOCAL_IMAGES_FILEPATH=/var/lib/rancher/rke2/agent/images
ARTIFACT_DIR="${BASE_SHARED_DIR}/local-artifacts/rke2"

mkdir -p ${LOCAL_IMAGES_FILEPATH}

cp ${ARTIFACT_DIR}/images/* ${LOCAL_IMAGES_FILEPATH}

# TODO - add ability to use local binary with yum install

# ----------------------------------------------------------
# uncomment to use a specific local binary for the install
# ----------------------------------------------------------
# LOCAL_RKE2_BIN='rke2-beta13-dev'

#if [ -n "${LOCAL_RKE2_BIN}" ] && [ -f "${ARTIFACT_DIR}/bin/${LOCAL_RKE2_BIN}" ] ; then
#  echo "Use "${ARTIFACT_DIR}/bin/${LOCAL_RKE2_BIN}" for rke2 binary"
#
#  INSTALL_RKE2_SKIP_START=true \
#    RKE2_RUNTIME_IMAGE="rancher/rke2-runtime:${RKE2_VERSION_DOCKER}" \
#    ${ARTIFACT_DIR}/bin/rke2-installer.run
#
#  rm -f /usr/local/bin/rke2
#
#  cp "${ARTIFACT_DIR}/bin/${LOCAL_RKE2_BIN}" /usr/local/bin/rke2
#
#  systemctl start rke2
#else
#  ${ARTIFACT_DIR}/bin/rke2-installer.run
#fi

yum install -y ${ARTIFACT_DIR}/rpm/*

systemctl enable rke2-server && systemctl start rke2-server

while [ -f "/etc/rancher/rke2/rke2.yaml" ] ; do
  echo "Waiting for /etc/rancher/rke2/rke2.yaml to exist..."
  sleep 10
done

chmod +r /etc/rancher/rke2/rke2.yaml

echo "RKE2 cluster is wrapping up installation, run the following commands to allow kubectl access:
export KUBECONFIG=/etc/rancher/rke2/rke2.yaml
export PATH=/var/lib/rancher/rke2/bin/:\${PATH}"
