#!/bin/bash

# Copyright 2026 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

if [ -z "${1}" ]; then
  echo "must provide architecture as first parameter"
  exit 1
fi

if [ -z "${2}" ]; then
  echo "must provide output path as second parameter"
  exit 1
fi

arch="${1}"
output="${2}"

echo "Downloading docker"

case "$arch" in \
    'amd64') \
        url='https://download.docker.com/linux/static/stable/x86_64/docker-27.5.1.tgz';
        ;;
    'arm')
        url='https://download.docker.com/linux/static/stable/armhf/docker-27.5.1.tgz';
        ;; \
    'arm64')
        url='https://download.docker.com/linux/static/stable/aarch64/docker-27.5.1.tgz';
        ;;
    *) echo >&2 "error: unsupported 'docker.tgz' architecture ($arch)"; exit 1 ;; 
esac;

wget -O 'docker.tgz' "$url";

tar --extract \
		--file docker.tgz \
		--strip-components 1 \
		--directory "$output" \
		--no-same-owner \
		'docker/docker'

rm docker.tgz;

echo "Downloading docker compose"

case "$arch" in
    'amd64')
        url='https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-x86_64';
        sha256='ed1917fb54db184192ea9d0717bcd59e3662ea79db48bff36d3475516c480a6b';
        ;;
    'arm')
        url='https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-armv7';
        sha256='c12bb3c23db5c409a15dbb13be4b61faa74c881d5db5f8a2816f60c19c35251a';
        ;;
    'arm64')
        url='https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-aarch64';
        sha256='0c4591cf3b1ed039adcd803dbbeddf757375fc08c11245b0154135f838495a2f';
        ;; 
    *) echo >&2 "warning: unsupported 'docker-compose' architecture ($arch); skipping"; exit 0 ;; 
esac;

wget -O 'docker-compose' "$url";
echo "$sha256 *"'docker-compose' | sha256sum -c -;
chmod +x docker-compose
mv -vT 'docker-compose' "$output/docker-compose";

