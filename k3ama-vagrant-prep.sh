
K3S_VERSION='v1.18.8%2Bk3s1'

ADDL_IMAGES=./local-artifacts/images
LOCAL_BIN=./local-artifacts/bin
LOCAL_RPM=./local-artifacts/rpm

mkdir -p ${ADDL_IMAGES}
mkdir -p ${LOCAL_BIN}

pushd ${ADDL_IMAGES}

curl -LO https://github.com/rancher/k3s/releases/download/${K3S_VERSION}/k3s-airgap-images-amd64.tar

popd

pushd ${LOCAL_BIN}

curl -LO https://github.com/rancher/k3s/releases/download/${K3S_VERSION}/k3s
chmod +x k3s
curl -L https://raw.githubusercontent.com/rancher/k3s/${K3S_VERSION}/install.sh -o k3s-install.sh
chmod +x k3s-install.sh

popd

pushd ${LOCAL_RPM}
curl -LO https://rpm.rancher.io/k3s-selinux-0.1.1-rc1.el7.noarch.rpm

# on machine with yum installed and internet access:
# yum install -y yum-utils
# yumdownloader --destdir=. --resolve container-selinux selinux-policy-base

popd
