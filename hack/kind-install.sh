#!/usr/bin/env bash

# Copyright 2023 The Kubernetes Authors.
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

# Create a kind cluster with proxy workarounds for Docker Desktop.
#
# When the host has HTTP_PROXY/HTTPS_PROXY pointing at localhost (e.g.
# localhost:10054), kind inherits those env vars into node containers.
# Inside the container, "localhost" refers to the container itself, not the
# host, so the proxy is unreachable and all image pulls fail with:
#   proxyconnect tcp: dial tcp [::1]:10054: connection refused
#
# This script unsets proxy env vars before creating the cluster and cleans
# up any proxy that leaks in via the Docker daemon configuration.

set -euo pipefail

CLUSTER_NAME="${1:-capi-test}"

echo "Creating kind cluster '${CLUSTER_NAME}'..."

# Unset proxy env vars so kind doesn't inject them into nodes.
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy NO_PROXY no_proxy 2>/dev/null || true

kind create cluster --name "${CLUSTER_NAME}" --wait 5m

# Detect and fix proxy inside the node if Docker daemon injected one.
NODE="${CLUSTER_NAME}-control-plane"
PROXY=$(docker exec "${NODE}" printenv HTTP_PROXY 2>/dev/null || true)
if [[ "${PROXY}" == *"localhost"* || "${PROXY}" == *"127.0.0.1"* || "${PROXY}" == *"[::1]"* ]]; then
  echo "Detected broken proxy (${PROXY}) inside kind node, clearing..."
  docker exec "${NODE}" bash -c '
    mkdir -p /etc/systemd/system/containerd.service.d
    cat > /etc/systemd/system/containerd.service.d/no-proxy.conf <<EOF
[Service]
UnsetEnvironment=HTTP_PROXY HTTPS_PROXY http_proxy https_proxy
EOF
    systemctl daemon-reload
    systemctl restart containerd
  '
  echo "Proxy cleared, containerd restarted."
fi

echo "Kind cluster '${CLUSTER_NAME}' is ready."
