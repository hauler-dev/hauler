#!/bin/sh

BASE_SHARED_DIR="/opt/k3ama"
VAGRANT_SCRIPTS_DIR="${BASE_SHARED_DIR}/vagrant-scripts"

for script in ${VAGRANT_SCRIPTS_DIR}/*-prep.sh ; do
  echo "---"
  echo "Running ${script} ..."
  echo "---"

  sh "${script}"
done
