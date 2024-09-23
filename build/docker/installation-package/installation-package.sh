#!/bin/bash

set -e

REPOSITORY=${REPOSITORY:-"swr.cn-north-4.myhuaweicloud.com/cloud-native-riscv64/installation-package"}
RELEASE_VERSION=$(git describe --tags)
pushTag="$1"
WORK_DIR=$(cd "$(dirname "$0")";pwd)
ARCHS=riscv64

if ! [ "$(nerdctl version)" ]; then
  echo "nerdctl check failed"
  exit 1
fi

for arch in "${ARCHS[@]}" ; do
  nerdctl build --no-cache --platform "$arch" -t "$REPOSITORY":"$RELEASE_VERSION"-"$arch" -f "$WORK_DIR/installation-package.dockerfile" -o type=docker .
done

if [ "$pushTag" = 'push' ]; then
  echo "push installation-package image"
  nerdctl push "$REPOSITORY":"$RELEASE_VERSION"
else
  echo 'image save in local'
fi

