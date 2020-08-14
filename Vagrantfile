##################################
# vagrant plugin install vagrant-vbguest
##################################


Vagrant.configure("2") do |config|
  config.vm.box = "centos/7"
  config.vm.hostname = "airgap"
  config.vm.network "private_network", type: "dhcp"
#    name: "vboxnet3"
#  config.vm.network "private_network", ip: "10.0.2.1",
#    auto_config: false
  config.vm.synced_folder ".","/opt/k3ama"
  
  config.vm.provider "virtualbox" do |vb|
   vb.memory = "1024"
   vb.cpus = "2"
  end
end
