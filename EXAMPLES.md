# Hauler usage examples

Below are some examples of using Hauler end-to-end.

One step that is very specific per user is the "move data into the airgap" process - that will be noted with a comment along with a placeholder of:
```bash
scp ./local-file.bin airgap-server:/remote-file.bin
```
This is _clearly_ not an actual airgapped setup, but it's a short clear indication of some boundary being crossed.

## In-repo examples

As shown in the README, some example hauler folders are provided for you. You can use these either via command-line arguments only, or via a config file.

### CLI args

```bash
# change to directory containing this repo
cd github/rancherfederal/hauler

hauler package build -p testdata/docker-registry -p testdata/rawmanifests

# pkg.tar.zst is generated
ls -lh pkg.tar.zst

# move binary and package into airgap
scp /usr/local/bin/hauler ./pkg.tar.zst user@airgap-server:/home/user/

# on airgap server, run hauler's installation process (root access needed)
sudo hauler package run pkg.tar.zst

# add hauler binaries to path and access cluster
export PATH="${PATH}:/opt/hauler/bin"
kubectl get nodes

# uninstall and clean up
sudo /opt/hauler/bin/k3s-uninstall.sh
sudo rm -rf /opt/hauler
```

### Config file

```bash
cd github/rancherfederal/hauler

cat <<EOF > hauler-pkg.yaml
apiVersion: hauler.cattle.io/v1alpha1
kind: Package
metadata:
  name: hauler-pkg
spec:
  driver:
    type: k3s
    version: v1.20.8+k3s1
  fleet:
    version: v0.3.5
  paths:
    - testdata/docker-registry
    - testdata/rawmanifests
EOF

hauler package build --config hauler-pkg.yaml --name hauler-pkg

# hauler-pkg.tar.zst is generated
ls -lh hauler-pkg.tar.zst

# move binary and package into airgap
scp /usr/local/bin/hauler ./hauler-pkg.tar.zst user@airgap-server:/home/user/

# on airgap server, run hauler's installation process (root access needed)
sudo hauler package run hauler-pkg.tar.zst

# add hauler binaries to path and access cluster
export PATH="${PATH}:/opt/hauler/bin"
kubectl get nodes

# uninstall and clean up
sudo /opt/hauler/bin/k3s-uninstall.sh
sudo rm -rf /opt/hauler
```
