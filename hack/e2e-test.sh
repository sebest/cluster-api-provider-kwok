#!/usr/bin/env bash

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

# End-to-end test for the CAPI KWOK provider.
#
# This script performs a full lifecycle test:
#   1. Creates a fresh Kind cluster and installs CAPI
#   2. Builds and deploys the kwok provider
#   3. Creates a workload cluster with a MachinePool (3 replicas)
#   4. Verifies all resources converge to ready state
#   5. Cleanup
#
# Usage:
#   hack/e2e-test.sh [--no-cleanup] [--timeout SECONDS] [--stop-after PHASE]
#
# Examples:
#   hack/e2e-test.sh --stop-after 3          # Stop after creating workload cluster
#   hack/e2e-test.sh --stop-after 2 --no-cleanup  # Stop after deploy, keep cluster

set -euo pipefail

# --- Configuration -----------------------------------------------------------

CLUSTER_NAME="capi-test"
WORKLOAD_CLUSTER_NAME="e2e-test"
TIMEOUT="${E2E_TIMEOUT:-300}"  # 5 minutes default
CLEANUP=true
STOP_AFTER=0  # 0 means run all phases
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-cleanup)
      CLEANUP=false
      shift
      ;;
    --timeout)
      TIMEOUT="$2"
      shift 2
      ;;
    --timeout=*)
      TIMEOUT="${1#*=}"
      shift
      ;;
    --stop-after)
      STOP_AFTER="$2"
      shift 2
      ;;
    --stop-after=*)
      STOP_AFTER="${1#*=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

# --- Helpers -----------------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

step() {
  echo -e "\n${CYAN}${BOLD}==> $1${RESET}"
}

info() {
  echo -e "    ${GREEN}$1${RESET}"
}

warn() {
  echo -e "    ${YELLOW}$1${RESET}"
}

fail() {
  echo -e "\n${RED}${BOLD}FAIL: $1${RESET}" >&2
  exit 1
}

wait_for() {
  local description="$1"
  shift
  info "Waiting for ${description} (timeout ${TIMEOUT}s)..."
  if ! "$@"; then
    fail "${description} did not become ready within ${TIMEOUT}s"
  fi
  info "${description} - OK"
}

check_stop() {
  local phase="$1"
  if [[ "${STOP_AFTER}" -gt 0 && "${phase}" -ge "${STOP_AFTER}" ]]; then
    echo -e "\n${GREEN}${BOLD}Stopped after phase ${phase} (--stop-after=${STOP_AFTER})${RESET}"
    exit 0
  fi
}

cleanup() {
  if [[ "${CLEANUP}" == "true" ]]; then
    step "Cleanup"
    kubectl delete cluster "${WORKLOAD_CLUSTER_NAME}" --ignore-not-found --timeout=60s 2>/dev/null || true
    kind delete cluster --name "${WORKLOAD_CLUSTER_NAME}" 2>/dev/null || true
    kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
    rm -f "${MGMT_KUBECONFIG:-}" 2>/dev/null || true
    rm -rf /tmp/capf-kwok 2>/dev/null || true
    info "Cleanup complete"
  else
    warn "Skipping cleanup (--no-cleanup). Kind cluster '${CLUSTER_NAME}' is still running."
  fi
}

# --- Phase 1: Setup ----------------------------------------------------------

step "Phase 1: Setup - Creating Kind cluster"

# Delete any existing cluster
kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
kind delete cluster --name "${WORKLOAD_CLUSTER_NAME}" 2>/dev/null || true

# Clean shared kwok working directory to avoid stale data from previous runs
rm -rf /tmp/capf-kwok

# Create fresh kind cluster using the existing helper script
"${ROOT_DIR}/hack/kind-install.sh" "${CLUSTER_NAME}"

# Verify we can talk to the cluster
kubectl cluster-info --context "kind-${CLUSTER_NAME}" >/dev/null 2>&1 \
  || fail "Cannot reach kind cluster"
info "Kind cluster '${CLUSTER_NAME}' is ready"

# Save management cluster kubeconfig to a dedicated file. When the workload
# kind cluster is created inside the controller, `kind create cluster`
# overwrites the default kubeconfig context. Using an explicit kubeconfig
# file avoids this problem.
MGMT_KUBECONFIG=$(mktemp)
kind get kubeconfig --name "${CLUSTER_NAME}" > "${MGMT_KUBECONFIG}"
export KUBECONFIG="${MGMT_KUBECONFIG}"

# Install CAPI core components
step "Phase 1: Setup - Installing CAPI core components"
clusterctl init --wait-providers
info "CAPI core components installed"

check_stop 1

# --- Phase 2: Build & Deploy -------------------------------------------------

step "Phase 2: Build & Deploy - Building and loading provider image"
cd "${ROOT_DIR}"
make kind-load CAPI_KIND_CLUSTER_NAME="${CLUSTER_NAME}"
info "Provider image loaded into kind"

step "Phase 2: Build & Deploy - Deploying kwok provider"
kubectl apply -k config/default

wait_for "capf-controller-manager to be Running" \
  kubectl wait deployment/capf-controller-manager \
    -n capf-system \
    --for=condition=Available \
    --timeout="${TIMEOUT}s"

check_stop 2

# --- Phase 3: Create Workload Cluster ----------------------------------------

step "Phase 3: Create workload cluster with MachinePool (3 replicas)"

kubectl apply -f - <<'EOF'
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: e2e-test
spec:
  clusterNetwork:
    pods:
      cidrBlocks: ["192.168.0.0/16"]
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: KwokCluster
    name: e2e-test
  controlPlaneRef:
    apiGroup: controlplane.cluster.x-k8s.io
    kind: KwokControlPlane
    name: e2e-test-control-plane
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: KwokCluster
metadata:
  name: e2e-test
spec: {}
---
apiVersion: controlplane.cluster.x-k8s.io/v1alpha1
kind: KwokControlPlane
metadata:
  name: e2e-test-control-plane
spec: {}
---
apiVersion: bootstrap.cluster.x-k8s.io/v1alpha1
kind: KwokConfig
metadata:
  name: e2e-test-pool-0
spec: {}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: KwokMachinePool
metadata:
  name: e2e-test-pool-0
spec: {}
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachinePool
metadata:
  name: e2e-test-pool-0
spec:
  clusterName: e2e-test
  replicas: 3
  template:
    spec:
      clusterName: e2e-test
      version: v1.31.0
      bootstrap:
        configRef:
          apiGroup: bootstrap.cluster.x-k8s.io
          kind: KwokConfig
          name: e2e-test-pool-0
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: KwokMachinePool
        name: e2e-test-pool-0
EOF

info "Workload cluster manifests applied"

check_stop 3

# --- Phase 4: Verification ---------------------------------------------------

step "Phase 4: Verification"

# 4a. Wait for KwokControlPlane to be Ready
wait_for "KwokControlPlane to be Ready" \
  kubectl wait kwokcontrolplane/e2e-test-control-plane \
    --for=jsonpath='{.status.ready}'=true \
    --timeout="${TIMEOUT}s"

# 4b. Wait for KwokMachinePool to be Ready
wait_for "KwokMachinePool to be Ready" \
  kubectl wait kwokmachinepool/e2e-test-pool-0 \
    --for=jsonpath='{.status.ready}'=true \
    --timeout="${TIMEOUT}s"

# 4c. Wait for 3 KwokMachine child objects
info "Waiting for 3 KwokMachine objects..."
DEADLINE=$((SECONDS + TIMEOUT))
while true; do
  COUNT=$(kubectl get kwokmachine -l cluster.x-k8s.io/cluster-name=e2e-test --no-headers 2>/dev/null | wc -l | tr -d ' ')
  if [[ "${COUNT}" -ge 3 ]]; then
    break
  fi
  if [[ ${SECONDS} -ge ${DEADLINE} ]]; then
    fail "Expected 3 KwokMachine objects, found ${COUNT}"
  fi
  sleep 2
done
info "3 KwokMachine objects exist - OK"

# 4d. Wait for 3 CAPI Machine objects to reach Running phase
info "Waiting for 3 CAPI Machines to reach Running phase..."
DEADLINE=$((SECONDS + TIMEOUT))
while true; do
  RUNNING=$(kubectl get machine -l cluster.x-k8s.io/cluster-name=e2e-test \
    -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null \
    | grep -c "Running") || true
  if [[ "${RUNNING}" -ge 3 ]]; then
    break
  fi
  if [[ ${SECONDS} -ge ${DEADLINE} ]]; then
    CURRENT=$(kubectl get machine -l cluster.x-k8s.io/cluster-name=e2e-test --no-headers 2>/dev/null || true)
    fail "Expected 3 Running machines, got:\n${CURRENT}"
  fi
  sleep 2
done
info "3 CAPI Machines are Running - OK"

# 4e. Verify kubeconfig secret exists and contains the container IP
#      (not 127.0.0.1, since the secret is for in-cluster access)
step "Phase 4: Verification - Kubeconfig"
KUBECONFIG_SECRET="e2e-test-kubeconfig"
kubectl get secret "${KUBECONFIG_SECRET}" >/dev/null 2>&1 \
  || fail "Kubeconfig secret '${KUBECONFIG_SECRET}' not found"
info "Kubeconfig secret exists - OK"

KUBECONFIG_DATA=$(kubectl get secret "${KUBECONFIG_SECRET}" -o jsonpath='{.data.value}' | base64 -d)
if echo "${KUBECONFIG_DATA}" | grep -q "127\.0\.0\.1"; then
  fail "Kubeconfig secret contains 127.0.0.1 — should use container IP for in-cluster access"
fi
info "Kubeconfig uses container IP (not 127.0.0.1) - OK"

# 4f. Verify 3 nodes exist in the workload cluster and are Ready
#     (using kind get kubeconfig for Mac-side access via Docker port-mapping)
step "Phase 4: Verification - Workload cluster nodes"
WORKLOAD_KUBECONFIG=$(mktemp)
kind get kubeconfig --name "${WORKLOAD_CLUSTER_NAME}" > "${WORKLOAD_KUBECONFIG}"

NODE_COUNT=0
DEADLINE=$((SECONDS + TIMEOUT))
while true; do
  NODE_COUNT=$(kubectl --kubeconfig "${WORKLOAD_KUBECONFIG}" get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ') || true
  if [[ "${NODE_COUNT}" -ge 3 ]]; then
    break
  fi
  if [[ ${SECONDS} -ge ${DEADLINE} ]]; then
    fail "Expected 3 nodes in workload cluster, found ${NODE_COUNT}"
  fi
  sleep 2
done
info "Workload cluster has ${NODE_COUNT} nodes - OK"

# 4g. Verify workload cluster nodes are Ready (not Unknown)
info "Waiting for workload cluster nodes to be Ready..."
DEADLINE=$((SECONDS + TIMEOUT))
while true; do
  READY_COUNT=$(kubectl --kubeconfig "${WORKLOAD_KUBECONFIG}" get nodes \
    --no-headers 2>/dev/null \
    | grep -c ' Ready' ) || true
  if [[ "${READY_COUNT}" -ge 3 ]]; then
    break
  fi
  if [[ ${SECONDS} -ge ${DEADLINE} ]]; then
    CURRENT_NODES=$(kubectl --kubeconfig "${WORKLOAD_KUBECONFIG}" get nodes 2>/dev/null || true)
    fail "Expected 3 Ready nodes, got:\n${CURRENT_NODES}"
  fi
  sleep 2
done
info "All ${READY_COUNT} workload cluster nodes are Ready - OK"

# Show final state
step "Final State"
echo ""
echo "--- Cluster ---"
kubectl get cluster "${WORKLOAD_CLUSTER_NAME}" 2>/dev/null || true
echo ""
echo "--- KwokControlPlane ---"
kubectl get kwokcontrolplane 2>/dev/null || true
echo ""
echo "--- MachinePool ---"
kubectl get machinepool 2>/dev/null || true
echo ""
echo "--- KwokMachinePool ---"
kubectl get kwokmachinepool 2>/dev/null || true
echo ""
echo "--- Machines ---"
kubectl get machine -l cluster.x-k8s.io/cluster-name=e2e-test 2>/dev/null || true
echo ""
echo "--- Workload Cluster Nodes ---"
kubectl --kubeconfig "${WORKLOAD_KUBECONFIG}" get nodes 2>/dev/null || true

rm -f "${WORKLOAD_KUBECONFIG}"

check_stop 4

# --- Phase 5: Cleanup --------------------------------------------------------

cleanup

echo -e "\n${GREEN}${BOLD}E2E test PASSED${RESET}"
