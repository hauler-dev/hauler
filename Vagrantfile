##################################
# The vagrant-vbguest plugin is required for CentOS 7.
# Run the following command to install/update this plugin:
# vagrant plugin install vagrant-vbguest
##################################

Vagrant.configure("2") do |config|
  config.vm.box = "centos/8"
  config.vm.hostname = "airgap"
  config.vm.network "private_network", type: "dhcp"

  config.vm.synced_folder ".", "/vagrant"

  config.vm.provider "virtualbox" do |vb|
    vb.memory = "2048"
    vb.cpus = "2"
  
  config.vm.provision "airgap", type: "shell", run: "always",
    inline: "/vagrant/vagrant-scripts/airgap.sh airgap"
  end

  # SELinux is Enforcing by default.
  # To set SELinux as Disabled on a VM that has already been provisioned:
  #   SELINUX=Disabled vagrant up --provision-with=selinux
  # To set SELinux as Permissive on a VM that has already been provsioned
  #   SELINUX=Permissive vagrant up --provision-with=selinux
  config.vm.provision "selinux", type: "shell", run: "once" do |sh|
    sh.upload_path = "/tmp/vagrant-selinux"
    sh.env = {
        'SELINUX': ENV['SELINUX'] || "Enforcing"
    }
    sh.inline = <<~SHELL
        #!/usr/bin/env bash
        set -eux -o pipefail

        if ! type -p getenforce setenforce &>/dev/null; then
          echo SELinux is Disabled
          exit 0
        fi

        case "${SELINUX}" in
          Disabled)
            if mountpoint -q /sys/fs/selinux; then
              setenforce 0
              umount -v /sys/fs/selinux
            fi
            ;;
          Enforcing)
            mountpoint -q /sys/fs/selinux || mount -o rw,relatime -t selinuxfs selinuxfs /sys/fs/selinux
            setenforce 1
            ;;
          Permissive)
            mountpoint -q /sys/fs/selinux || mount -o rw,relatime -t selinuxfs selinuxfs /sys/fs/selinux
            setenforce 0
            ;;
          *)
            echo "SELinux mode not supported: ${SELINUX}" >&2
            exit 1
            ;;
        esac

        echo SELinux is $(getenforce)
    SHELL
  end
end
