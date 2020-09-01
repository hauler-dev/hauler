#!/bin/sh

################################################################################
# RUN IN VAGRANT MACHINE
# Install a default, bare k3s cluster into the Vagrant machine
################################################################################

if [ -f "/usr/local/bin/k3s-uninstall.sh" ]; then
  /usr/local/bin/k3s-uninstall.sh
else
  echo "k3s is not installed"
fi

if pgrep -x "firewalld" >/dev/null
then
  echo "[FATAL] disable firewalld first"
fi

SELINUXSTATUS=$(getenforce)
if [ "$SELINUXSTATUS" == "Permissive" ]; then
    echo "[FATAL] disable selinux"
    exit 1
else
    echo "SELINUX disabled. continuing"
fi

LOCAL_IMAGES_FILEPATH=/var/lib/rancher/k3s/agent/images
ARTIFACT_DIR=/opt/k3ama/local-artifacts/k3s

mkdir -p ${LOCAL_IMAGES_FILEPATH}

cp ${ARTIFACT_DIR}/images/* ${LOCAL_IMAGES_FILEPATH}

cp ${ARTIFACT_DIR}/bin/k3s /usr/local/bin/k3s
chmod +x /usr/local/bin/k3s

yum install -y ${ARTIFACT_DIR}/rpm/*

INSTALL_K3S_SKIP_DOWNLOAD=true ${ARTIFACT_DIR}/bin/k3s-install.sh

chmod +r /etc/rancher/k3s/k3s.yaml
