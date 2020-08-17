#!/bin/bash

IMAGE_NAME="$1"
SAVE_DIR="$2"

if [ -z "${IMAGE_NAME}" ]; then
  echo "[Usage] ./save-docker-image.sh <image_name>"
  exit 1
fi

if [ -z "$2" ]; then
  SAVE_DIR="."

fi

echo "Creating ${IMAGE_NAME} backup..."
#docker save ${IMAGE_NAME} | gzip --stdout > ${SAVE_DIR}/${IMAGE_NAME}.tgz
docker save ${IMAGE_NAME} > ${IMAGE_NAME}.tar
