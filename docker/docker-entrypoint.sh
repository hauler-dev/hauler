#!/bin/sh
set -e

# Load all haul tarball files if they exist
for haul_file in /home/hauler/*.tar.zst; do
    if [ -f "$haul_file" ]; then
        echo "Loading Haul: $haul_file"
        /usr/local/bin/hauler store load "$haul_file"
    fi
done

# Check if Hauler needs to sync using a manifest file
if [ -f "/home/hauler/hauler-manifest.yaml" ]; then
    /usr/local/bin/hauler store sync -f /home/hauler/hauler-manifest.yaml
fi

if [ -d "/home/hauler/store" ]; then
  echo "Store directory found, starting the Hauler registry server..."
  /usr/local/bin/hauler store serve registry --port=5000
else
  echo "Store directory not found."
  echo "Adding alpine:3.19 image to the store..."
  hauler store add image alpine:3.19
  echo "Starting the Hauler registry server..."
  /usr/local/bin/hauler store serve registry --port=5000
fi


# # Start the Hauler fileserver
# /usr/local/bin/hauler serve fileserver --port=8080
