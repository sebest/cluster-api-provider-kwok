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

# Interactive demo for the CAPI KWOK provider.
#
# This script walks through the CAPI reconciliation flow step-by-step,
# applying each KWOK CRD resource one at a time with narration explaining
# the CAPI contract, verification commands between each step, and
# interactive pauses.
#
# Usage:
#   hack/demo.sh [--no-cleanup] [--no-pause] [--timeout SECONDS] [--stop-after STEP]
#
# Flags:
#   --no-cleanup    Leave clusters running after demo (default: cleanup)
#   --no-pause      Skip interactive pauses (useful for CI)
#   --timeout N     Timeout in seconds for waits (default: 300)
#   --stop-after N  Stop after step N (default: 0 = run all steps)
#
# Examples:
#   hack/demo.sh                            # Full interactive demo
#   hack/demo.sh --no-pause --no-cleanup    # Non-interactive, keep clusters
#   hack/demo.sh --stop-after 3             # Stop after creating the Cluster

set -euo pipefail

# --- Configuration -----------------------------------------------------------

CLUSTER_NAME="capi-test"
WORKLOAD_CLUSTER_NAME="demo-cluster"
TIMEOUT="${DEMO_TIMEOUT:-300}"
CLEANUP=true
PAUSE=true
STOP_AFTER=0  # 0 means run all steps
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-cleanup)
      CLEANUP=false
      shift
      ;;
    --no-pause)
      PAUSE=false
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
    -h|--help)
      head -37 "$0" | tail -20
      exit 0
      ;;
    *)
      shift
      ;;
  esac
done

# --- Helpers (reused from e2e-test.sh) ---------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
DIM='\033[2m'
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

# --- Demo-specific helpers ---------------------------------------------------

narrate() {
  echo -e "\n    ${MAGENTA}${BOLD}$1${RESET}"
}

detail() {
  echo -e "      ${DIM}$1${RESET}"
}

separator() {
  echo -e "\n${DIM}────────────────────────────────────────────────────────────────${RESET}"
}

pause() {
  if [[ "${PAUSE}" == "true" ]]; then
    echo ""
    read -r -p "    Press Enter to continue..."
  fi
}

run_cmd() {
  echo -e "    ${YELLOW}\$ $1${RESET}"
  # Run the command and indent the output
  eval "$1" 2>&1 | sed 's/^/      /' || true
}

show_yaml() {
  echo -e "\n    ${BOLD}Manifest:${RESET}"
  echo -e "${DIM}"
  echo "$1" | sed 's/^/      /'
  echo -e "${RESET}"
}

check_stop() {
  local current_step="$1"
  if [[ "${STOP_AFTER}" -gt 0 && "${current_step}" -ge "${STOP_AFTER}" ]]; then
    echo -e "\n${GREEN}${BOLD}Stopped after step ${current_step} (--stop-after=${STOP_AFTER})${RESET}"
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

# ==============================================================================
#  Phase 1: Kind cluster + CAPI core install
# ==============================================================================

step "Phase 1: Setup - Creating Kind cluster and installing CAPI"

narrate "This demo walks through the CAPI reconciliation flow step-by-step."
detail "Each KWOK resource is applied individually so you can observe how"
detail "CAPI's OwnerRef mechanism triggers reconciliation in the controllers."
echo ""
narrate "Phase 1 creates the management cluster and installs CAPI core:"
echo ""
echo -e "${DIM}"
cat <<'DIAGRAM'
      ┌─────────────────────────────────────────────────────────┐
      │              Management Kind Cluster                    │
      │                  ("capi-test")                          │
      │                                                         │
      │  ┌───────────────────────────────────────────────────┐  │
      │  │              CAPI Core Components                 │  │
      │  │                                                   │  │
      │  │  ┌─────────────┐  ┌──────────────────────────┐   │  │
      │  │  │   Cluster   │  │  MachinePool / Machine   │   │  │
      │  │  │  Controller │  │      Controllers         │   │  │
      │  │  └─────────────┘  └──────────────────────────┘   │  │
      │  │                                                   │  │
      │  │  Watches Cluster, sets OwnerRefs on infra +       │  │
      │  │  control-plane refs, manages Machine lifecycle    │  │
      │  └───────────────────────────────────────────────────┘  │
      │                                                         │
      │  (No infrastructure provider installed yet)             │
      └─────────────────────────────────────────────────────────┘
DIAGRAM
echo -e "${RESET}"

pause

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

# Save management cluster kubeconfig to a dedicated file
MGMT_KUBECONFIG=$(mktemp)
kind get kubeconfig --name "${CLUSTER_NAME}" > "${MGMT_KUBECONFIG}"
export KUBECONFIG="${MGMT_KUBECONFIG}"

# Install CAPI core components
info "Installing CAPI core components..."
clusterctl init --wait-providers
info "CAPI core components installed"

# ==============================================================================
#  Phase 2: Build & deploy provider
# ==============================================================================

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

separator
narrate "Infrastructure is ready. Here is what we built:"
echo ""
echo -e "${DIM}"
cat <<'DIAGRAM'
      ┌─────────────────────────────────────────────────────────────┐
      │                Management Kind Cluster                      │
      │                    ("capi-test")                             │
      │                                                             │
      │  ┌───────────────────────────────────────────────────────┐  │
      │  │              CAPI Core  (clusterctl init)             │  │
      │  │                                                       │  │
      │  │  Cluster Controller ── sets OwnerRefs on infra/CP     │  │
      │  │  MachinePool Controller ── manages Machine lifecycle  │  │
      │  └───────────────────────────────────────────────────────┘  │
      │                          │                                  │
      │                   watches CRDs                              │
      │                          ▼                                  │
      │  ┌───────────────────────────────────────────────────────┐  │
      │  │         KWOK Provider  (capf-controller-manager)      │  │
      │  │                                                       │  │
      │  │  ┌──────────────┐ ┌───────────────┐ ┌─────────────┐  │  │
      │  │  │ KwokCluster  │ │KwokControlPlane│ │  KwokConfig │  │  │
      │  │  │  Controller  │ │  Controller   │ │  Controller │  │  │
      │  │  └──────────────┘ └───────────────┘ └─────────────┘  │  │
      │  │  ┌──────────────────┐ ┌──────────────┐               │  │
      │  │  │ KwokMachinePool  │ │  KwokMachine │               │  │
      │  │  │   Controller     │ │  Controller  │               │  │
      │  │  └──────────────────┘ └──────────────┘               │  │
      │  └───────────────────────────────────────────────────────┘  │
      │                                                             │
      │  No workload resources yet — let's create them step by step │
      └─────────────────────────────────────────────────────────────┘
DIAGRAM
echo -e "${RESET}"

narrate "Now let's apply resources one by one."
pause

# ==============================================================================
#  Phase 3: Step-by-step resource application
# ==============================================================================

# ── Step 1: KwokCluster ─────────────────────────────────────────────────────

separator
step "Step 1 of 7: Apply KwokCluster"

narrate "The infrastructure provider's cluster resource."
detail "Without an owning Cluster, the controller skips reconciliation."
detail "The controller calls GetOwnerCluster(), returns nil, and logs:"
detail "  \"Cluster Controller has not yet set OwnerRef\""

pause

MANIFEST=$(cat <<EOF
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: KwokCluster
metadata:
  name: ${WORKLOAD_CLUSTER_NAME}
spec: {}
EOF
)
show_yaml "$MANIFEST"
echo "$MANIFEST" | kubectl apply -f -

info "KwokCluster '${WORKLOAD_CLUSTER_NAME}' created"
echo ""

run_cmd "kubectl get kwokcluster"

narrate "Notice: the KwokCluster exists but is not ready — no owning Cluster yet."

pause
check_stop 1

# ── Step 2: KwokControlPlane ────────────────────────────────────────────────

separator
step "Step 2 of 7: Apply KwokControlPlane"

narrate "The control plane provider."
detail "Creates the workload kind cluster and generates the kubeconfig Secret."
detail "Like KwokCluster, it also waits for an owning Cluster before reconciling."

pause

MANIFEST=$(cat <<EOF
apiVersion: controlplane.cluster.x-k8s.io/v1alpha1
kind: KwokControlPlane
metadata:
  name: ${WORKLOAD_CLUSTER_NAME}-control-plane
spec: {}
EOF
)
show_yaml "$MANIFEST"
echo "$MANIFEST" | kubectl apply -f -

info "KwokControlPlane '${WORKLOAD_CLUSTER_NAME}-control-plane' created"
echo ""

run_cmd "kubectl get kwokcontrolplane"

narrate "The KwokControlPlane exists but is not ready — still waiting for its Cluster owner."

pause
check_stop 2

# ── Step 3: Cluster (the trigger) ───────────────────────────────────────────

separator
step "Step 3 of 7: Apply CAPI Cluster (the trigger!)"

narrate "The top-level CAPI Cluster resource."
detail "References KwokCluster via infrastructureRef and KwokControlPlane via controlPlaneRef."
detail "When created, CAPI sets OwnerRefs on both — THIS triggers reconciliation."
detail ""
detail "What happens next:"
detail "  1. KwokCluster sees its OwnerRef → sets status.ready = true"
detail "  2. KwokControlPlane sees its OwnerRef → creates a real kind cluster"
detail "  3. KwokControlPlane generates the kubeconfig Secret"
detail "  4. Cluster reaches Phase=Provisioned"

pause

MANIFEST=$(cat <<EOF
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: ${WORKLOAD_CLUSTER_NAME}
spec:
  clusterNetwork:
    pods:
      cidrBlocks: ["192.168.0.0/16"]
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: KwokCluster
    name: ${WORKLOAD_CLUSTER_NAME}
  controlPlaneRef:
    apiGroup: controlplane.cluster.x-k8s.io
    kind: KwokControlPlane
    name: ${WORKLOAD_CLUSTER_NAME}-control-plane
EOF
)
show_yaml "$MANIFEST"
echo "$MANIFEST" | kubectl apply -f -

info "CAPI Cluster '${WORKLOAD_CLUSTER_NAME}' created — reconciliation triggered!"
echo ""

# Wait for KwokCluster to become ready
narrate "Watching KwokCluster become Ready..."
wait_for "KwokCluster to be Ready" \
  kubectl wait kwokcluster/${WORKLOAD_CLUSTER_NAME} \
    --for=jsonpath='{.status.ready}'=true \
    --timeout="${TIMEOUT}s"

run_cmd "kubectl get kwokcluster"
echo ""

# Wait for KwokControlPlane to become ready (creates real kind cluster)
narrate "Watching KwokControlPlane become Ready (this creates a kind cluster)..."
wait_for "KwokControlPlane to be Ready" \
  kubectl wait kwokcontrolplane/${WORKLOAD_CLUSTER_NAME}-control-plane \
    --for=jsonpath='{.status.ready}'=true \
    --timeout="${TIMEOUT}s"

run_cmd "kubectl get kwokcontrolplane"
echo ""

# Verify kubeconfig secret
narrate "Verifying kubeconfig Secret was created..."
run_cmd "kubectl get secret ${WORKLOAD_CLUSTER_NAME}-kubeconfig"
echo ""

# Show cluster status
narrate "Cluster should now be Provisioned:"
run_cmd "kubectl get cluster"

pause
check_stop 3

# ── Step 4: KwokConfig ──────────────────────────────────────────────────────

separator
step "Step 4 of 7: Apply KwokConfig (bootstrap provider)"

narrate "The bootstrap provider."
detail "In real providers (e.g., kubeadm), this generates cloud-init data."
detail "For KWOK, it creates an empty bootstrap Secret."
detail "It waits for an owning Machine or MachinePool before reconciling."

pause

MANIFEST=$(cat <<EOF
apiVersion: bootstrap.cluster.x-k8s.io/v1alpha1
kind: KwokConfig
metadata:
  name: ${WORKLOAD_CLUSTER_NAME}-pool-0
spec: {}
EOF
)
show_yaml "$MANIFEST"
echo "$MANIFEST" | kubectl apply -f -

info "KwokConfig '${WORKLOAD_CLUSTER_NAME}-pool-0' created"
echo ""

run_cmd "kubectl get kwokconfig"

narrate "The KwokConfig exists but is not ready — no owning Machine or MachinePool yet."

pause
check_stop 4

# ── Step 5: KwokMachinePool ─────────────────────────────────────────────────

separator
step "Step 5 of 7: Apply KwokMachinePool"

narrate "The infrastructure machine pool."
detail "Creates child KwokMachine objects (one per replica)."
detail "Like the other infra resources, it waits for an owning MachinePool."

pause

MANIFEST=$(cat <<EOF
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: KwokMachinePool
metadata:
  name: ${WORKLOAD_CLUSTER_NAME}-pool-0
spec: {}
EOF
)
show_yaml "$MANIFEST"
echo "$MANIFEST" | kubectl apply -f -

info "KwokMachinePool '${WORKLOAD_CLUSTER_NAME}-pool-0' created"
echo ""

run_cmd "kubectl get kwokmachinepool"

narrate "The KwokMachinePool exists but is not ready — no owning MachinePool yet."

pause
check_stop 5

# ── Step 6: MachinePool (triggers machine lifecycle) ─────────────────────────

separator
step "Step 6 of 7: Apply CAPI MachinePool (triggers machine lifecycle!)"

narrate "The CAPI MachinePool references KwokConfig and KwokMachinePool."
detail "CAPI sets OwnerRefs on both — triggering the full machine lifecycle cascade:"
detail ""
detail "  1. KwokConfig → creates bootstrap Secret"
detail "  2. KwokMachinePool → creates 3 child KwokMachine objects"
detail "  3. Each KwokMachine registers a fake Node in the workload cluster"
detail "  4. CAPI creates 3 Machine objects in Running phase"
detail "  5. 3 Nodes appear Ready in the workload cluster"

pause

MANIFEST=$(cat <<EOF
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachinePool
metadata:
  name: ${WORKLOAD_CLUSTER_NAME}-pool-0
spec:
  clusterName: ${WORKLOAD_CLUSTER_NAME}
  replicas: 3
  template:
    spec:
      clusterName: ${WORKLOAD_CLUSTER_NAME}
      version: v1.31.0
      bootstrap:
        configRef:
          apiGroup: bootstrap.cluster.x-k8s.io
          kind: KwokConfig
          name: ${WORKLOAD_CLUSTER_NAME}-pool-0
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: KwokMachinePool
        name: ${WORKLOAD_CLUSTER_NAME}-pool-0
EOF
)
show_yaml "$MANIFEST"
echo "$MANIFEST" | kubectl apply -f -

info "CAPI MachinePool '${WORKLOAD_CLUSTER_NAME}-pool-0' created — machine lifecycle triggered!"
echo ""

# Sub-step 1: KwokConfig becomes Ready
narrate "1/5  KwokConfig should become Ready (bootstrap Secret created)..."
wait_for "KwokConfig to be Ready" \
  kubectl wait kwokconfig/${WORKLOAD_CLUSTER_NAME}-pool-0 \
    --for=jsonpath='{.status.ready}'=true \
    --timeout="${TIMEOUT}s"
run_cmd "kubectl get kwokconfig"
echo ""

# Sub-step 2: KwokMachinePool creates 3 child KwokMachines
narrate "2/5  Waiting for 3 KwokMachine child objects..."
DEADLINE=$((SECONDS + TIMEOUT))
while true; do
  COUNT=$(kubectl get kwokmachine -l cluster.x-k8s.io/cluster-name=${WORKLOAD_CLUSTER_NAME} --no-headers 2>/dev/null | wc -l | tr -d ' ')
  if [[ "${COUNT}" -ge 3 ]]; then
    break
  fi
  if [[ ${SECONDS} -ge ${DEADLINE} ]]; then
    fail "Expected 3 KwokMachine objects, found ${COUNT}"
  fi
  sleep 2
done
info "3 KwokMachine objects exist - OK"
run_cmd "kubectl get kwokmachine -l cluster.x-k8s.io/cluster-name=${WORKLOAD_CLUSTER_NAME}"
echo ""

# Sub-step 3: KwokMachinePool becomes Ready
narrate "3/5  KwokMachinePool should become Ready..."
wait_for "KwokMachinePool to be Ready" \
  kubectl wait kwokmachinepool/${WORKLOAD_CLUSTER_NAME}-pool-0 \
    --for=jsonpath='{.status.ready}'=true \
    --timeout="${TIMEOUT}s"
run_cmd "kubectl get kwokmachinepool"
echo ""

# Sub-step 4: 3 CAPI Machines reach Running phase
narrate "4/5  Waiting for 3 CAPI Machines to reach Running phase..."
DEADLINE=$((SECONDS + TIMEOUT))
while true; do
  RUNNING=$(kubectl get machine -l cluster.x-k8s.io/cluster-name=${WORKLOAD_CLUSTER_NAME} \
    -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null \
    | grep -c "Running") || true
  if [[ "${RUNNING}" -ge 3 ]]; then
    break
  fi
  if [[ ${SECONDS} -ge ${DEADLINE} ]]; then
    CURRENT=$(kubectl get machine -l cluster.x-k8s.io/cluster-name=${WORKLOAD_CLUSTER_NAME} --no-headers 2>/dev/null || true)
    fail "Expected 3 Running machines, got:\n${CURRENT}"
  fi
  sleep 2
done
info "3 CAPI Machines are Running - OK"
run_cmd "kubectl get machine -l cluster.x-k8s.io/cluster-name=${WORKLOAD_CLUSTER_NAME}"
echo ""

# Sub-step 5: 3 Nodes appear in workload cluster and are Ready
narrate "5/5  Verifying Nodes in the workload cluster..."
WORKLOAD_KUBECONFIG=$(mktemp)
kind get kubeconfig --name "${WORKLOAD_CLUSTER_NAME}" > "${WORKLOAD_KUBECONFIG}"

DEADLINE=$((SECONDS + TIMEOUT))
while true; do
  READY_COUNT=$(kubectl --kubeconfig "${WORKLOAD_KUBECONFIG}" get nodes \
    --no-headers 2>/dev/null \
    | grep -c ' Ready') || true
  if [[ "${READY_COUNT}" -ge 3 ]]; then
    break
  fi
  if [[ ${SECONDS} -ge ${DEADLINE} ]]; then
    CURRENT_NODES=$(kubectl --kubeconfig "${WORKLOAD_KUBECONFIG}" get nodes 2>/dev/null || true)
    fail "Expected 3 Ready nodes, got:\n${CURRENT_NODES}"
  fi
  sleep 2
done
info "All 3 workload cluster nodes are Ready - OK"
run_cmd "kubectl --kubeconfig ${WORKLOAD_KUBECONFIG} get nodes"

pause
check_stop 6

# ── Step 7: Final State Summary ──────────────────────────────────────────────

separator
step "Step 7 of 7: Final State Summary"

narrate "All resources have converged. Here is the complete state:"
echo ""

echo -e "    ${BOLD}--- Cluster ---${RESET}"
run_cmd "kubectl get cluster"
echo ""

echo -e "    ${BOLD}--- KwokCluster ---${RESET}"
run_cmd "kubectl get kwokcluster"
echo ""

echo -e "    ${BOLD}--- KwokControlPlane ---${RESET}"
run_cmd "kubectl get kwokcontrolplane"
echo ""

echo -e "    ${BOLD}--- KwokConfig ---${RESET}"
run_cmd "kubectl get kwokconfig"
echo ""

echo -e "    ${BOLD}--- MachinePool ---${RESET}"
run_cmd "kubectl get machinepool"
echo ""

echo -e "    ${BOLD}--- KwokMachinePool ---${RESET}"
run_cmd "kubectl get kwokmachinepool"
echo ""

echo -e "    ${BOLD}--- KwokMachines ---${RESET}"
run_cmd "kubectl get kwokmachine -l cluster.x-k8s.io/cluster-name=${WORKLOAD_CLUSTER_NAME}"
echo ""

echo -e "    ${BOLD}--- CAPI Machines ---${RESET}"
run_cmd "kubectl get machine -l cluster.x-k8s.io/cluster-name=${WORKLOAD_CLUSTER_NAME}"
echo ""

echo -e "    ${BOLD}--- Workload Cluster Nodes ---${RESET}"
run_cmd "kubectl --kubeconfig ${WORKLOAD_KUBECONFIG} get nodes"
echo ""

echo -e "    ${BOLD}--- Secrets ---${RESET}"
run_cmd "kubectl get secret ${WORKLOAD_CLUSTER_NAME}-kubeconfig"
echo ""

rm -f "${WORKLOAD_KUBECONFIG}"

separator
narrate "Reconciliation Flow Recap:"
echo ""
detail "  1.  KwokCluster created         — controller waiting for OwnerRef"
detail "  2.  KwokControlPlane created     — controller waiting for OwnerRef"
detail "  3.  CAPI Cluster created         — sets OwnerRefs on both"
detail "  4.  KwokCluster reconciles       — status.ready = true"
detail "  5.  KwokControlPlane reconciles  — creates kind cluster + kubeconfig Secret"
detail "  6.  KwokConfig created           — controller waiting for OwnerRef"
detail "  7.  KwokMachinePool created      — controller waiting for OwnerRef"
detail "  8.  CAPI MachinePool created     — sets OwnerRefs, triggers cascade"
detail "  9.  KwokMachinePool reconciles   — creates 3 KwokMachine children"
detail "  10. Machines reach Running       — 3 Nodes appear Ready in workload cluster"

pause
check_stop 7

# ==============================================================================
#  Phase 4: Cleanup
# ==============================================================================

cleanup

echo -e "\n${GREEN}${BOLD}Demo complete!${RESET}"
