#!/bin/sh
set -x

if [ "$#" -ne 1 ] || ( [ "$1" != "internet" ] && [ "$1" != "airgap" ] ); then
  echo \
"Enable or disable internet access in hauler's CentOS Vagrant machine.

Usage: $0 internet
       $0 airgap" >&2
  exit 1
fi

if [ "$1" = "internet" ]; then
  # internet: set default gateway to NAT network interface
  default_iface="eth0"
  gw_ip="10.0.2.2"
else
  # airgap: set default gateway to private network interface
  default_iface="eth1"
  gw_ip=$(ip -f inet a show "${default_iface}" | awk 'match($0, /inet ([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})/, arr) { print arr[1] }')
fi


ip r delete default
ip r add default via ${gw_ip} dev ${default_iface} proto dhcp metric 100
