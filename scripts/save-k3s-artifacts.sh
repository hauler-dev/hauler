#!/bin/bash

K3S_RELEASE="v1.18.8-rc1%2Bk3s1"

SAVE_DIR="$1"

if [ -z "$1" ]; then
  SAVE_DIR="."
fi

# k3s - arm64
wget -P ${SAVE_DIR} https://github.com/rancher/k3s/releases/download/${K3S_VERSION}/k3s-arm64

# k3s - amd64
wget -P ${SAVE_DIR} https://github.com/rancher/k3s/releases/download/${K3S_VERSION}/k3s

# images - amd64
wget -P ${SAVE_DIR} https://github.com/rancher/k3s/releases/download/${K3S_VERSION}/k3s-airgap-images-amd64.tar

# images - arm64
wget -P ${SAVE_DIR} https://github.com/rancher/k3s/releases/download/${K3S_VERSION}/k3s-airgap-images-arm64.tar

# images.txt
wget -P ${SAVE_DIR} https://github.com/rancher/k3s/releases/download/${K3S_VERSION}/k3s-images.txt

