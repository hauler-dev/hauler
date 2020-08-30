##################################
# The vagrant-vbguest plugin is required for CentOS 7.
# Run the following command to install/update this plugin:
# vagrant plugin install vagrant-vbguest
##################################

Vagrant.configure("2") do |config|
  config.vm.box = "centos/7"
  config.vm.hostname = "airgap"
  config.vm.network "private_network", type: "dhcp"
  config.vm.provision "shell",
    run: "always",
    inline: "ip route delete default && \
      gw_ip=$(ip -f inet a show eth1 | awk 'match($0, /inet ([0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3})/, arr) { print arr[1] }')
      ip r add default via $gw_ip dev eth1 proto dhcp metric 100"

  config.vm.synced_folder ".", "/opt/k3ama"

  config.vm.provider "virtualbox" do |vb|
    vb.memory = "1024"
    vb.cpus = "2"
  end
end
