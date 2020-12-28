## Hauler Vagrant machine

A Vagrantfile is provided to allow easy provisioning of a local air-gapped CentOS environment. Some artifacts need to be collected from the internet; below are the steps required for successfully provisioning this machine, downloading all dependencies, and installing k3s (without hauler) into this machine.

### First-time setup

1. Install vagrant, if needed: <https://www.vagrantup.com/downloads>
2. Install `vagrant-vbguest` plugin, as noted in the Vagrantfile:
   ```shell
   vagrant plugin install vagrant-vbguest
   ```
3. Deploy Vagrant machine, disabling SELinux:
   ```shell
   SELINUX=Disabled vagrant up
   ```
4. Access the Vagrant machine via SSH:
   ```shell
   vagrant ssh
   ```
5. Run all prep scripts inside of the Vagrant machine:
    > This script temporarily enables internet access from within the VM to allow downloading all dependencies. Even so, the air-gapped network configuration IS restored before completion.
   ```shell
   sudo /opt/hauler/vagrant-scripts/prep-all.sh
   ```

All dependencies for all `vagrant-scripts/*-install.sh` scripts are now downloaded to the local
repository under `local-artifacts`.

### Installing k3s manually

1. Access the Vagrant machine via SSH:
  ```bash
  vagrant ssh
  ```
2. Run the k3s install script inside of the Vagrant machine:
  ```shell
  sudo /opt/hauler/vagrant-scripts/k3s-install.sh
  ```

### Installing RKE2 manually

1. Access the Vagrant machine via SSH:
  ```shell
  vagrant ssh
  ```
2. Run the RKE2 install script inside of the Vagrant machine:
  ```shell
  sudo /opt/hauler/vagrant-scripts/rke2-install.sh
  ```
