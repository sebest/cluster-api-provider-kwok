# Cluster API Provider KWOK

A [Cluster API](https://cluster-api.sigs.k8s.io/) infrastructure provider that uses [KWOK](https://kwok.sigs.k8s.io/) (Kubernetes WithOut Kubelet) to simulate Kubernetes clusters. This enables fast, lightweight cluster lifecycle testing without real nodes or cloud infrastructure.

KWOK simulates Kubernetes nodes and pods without running actual kubelet processes, making it ideal for:

- **Testing Cluster API controllers** without cloud costs
- **CI/CD pipelines** that need fast cluster provisioning
- **Development** of Cluster API-based tooling

## Prerequisites

- A Kubernetes cluster with [Cluster API](https://cluster-api.sigs.k8s.io/user/quick-start.html) installed (`clusterctl init`)
- [Helm](https://helm.sh/) 3.x (for installation) or Go 1.24+ (for development)
- Docker
- kubectl

---

## User Guide

### Initialize Cluster API

Before installing the provider, ensure the core Cluster API components are installed:

```sh
clusterctl init
```

This installs the core CRDs (`Cluster`, `Machine`, etc.) that the provider depends on.

### Install with Helm

```sh
helm install capkw charts/cluster-api-provider-kwok/ \
  --namespace capkw-system --create-namespace
```

By default, the chart uses the image `docker.io/sebest/cluster-api-provider-kwok:dev`. To customize the installation, override values:

```sh
helm install capkw charts/cluster-api-provider-kwok/ \
  --namespace capkw-system --create-namespace \
  --set image.tag=v0.1.0 \
  --set image.pullPolicy=IfNotPresent
```

### Create a Simulated Cluster

With Cluster API initialized and the provider installed, use the included cluster template:

```sh
export CLUSTER_NAME=my-kwok-cluster
clusterctl generate cluster ${CLUSTER_NAME} \
  --from templates/cluster-template.yaml \
  | kubectl apply -f -
```

This creates a `Cluster`, `KwokCluster`, and `KwokControlPlane`. The provider will simulate the full cluster lifecycle.

### Configuration

#### Runtime Options

The `KwokCluster` spec supports different runtimes via the `runtime` field:

| Runtime | Description |
|---------|-------------|
| `docker` (default) | Uses Docker Compose to run KWOK components |
| `kind` | Uses kind to run a simulated cluster |
| `binary` | Runs KWOK components as local binaries |

#### SimulationConfig

All KWOK resources support a `simulationConfig` block for injecting latency into reconciliation:

```yaml
spec:
  simulationConfig:
    reconcile:
      latency: "30s"
```

### CRD Reference

| CRD | API Group | Status | Description |
|-----|-----------|--------|-------------|
| `KwokCluster` | `infrastructure.cluster.x-k8s.io` | Active | Represents a simulated cluster's infrastructure |
| `KwokMachine` | `infrastructure.cluster.x-k8s.io` | Active | Represents a simulated machine/node |
| `KwokMachinePool` | `infrastructure.cluster.x-k8s.io` | Active | Represents a pool of simulated machines |
| `KwokMachineTemplate` | `infrastructure.cluster.x-k8s.io` | Active | Template for creating KwokMachines (used by MachineDeployments) |
| `KwokControlPlane` | `controlplane.cluster.x-k8s.io` | Active | Manages the simulated control plane |
| `KwokConfig` | `bootstrap.cluster.x-k8s.io` | Active | Bootstrap configuration for KWOK nodes |

---

## Developer Guide

### Build

```sh
make managers
```

### Install CRDs

CRD manifests must be generated before they can be applied. Either run code generation first:

```sh
make generate-manifests
kubectl apply -f config/crd/bases/
```

Or use the Helm install (above), which bundles the CRDs automatically.

### Run the Controller Locally

```sh
go run . --health-addr=:9440
```

### Code Generation

Regenerate CRDs, RBAC manifests, and deepcopy functions after modifying API types:

```sh
make generate
```

Or run individual targets:

```sh
make generate-manifests   # CRD and RBAC manifests
make generate-go-deepcopy # deepcopy functions
```

### Testing

```sh
make test
```

> **Note:** `make test` requires `setup-envtest`, which is not yet fully configured in the Makefile (the `SETUP_ENVTEST_BIN`, `SETUP_ENVTEST_VER`, and `SETUP_ENVTEST_PKG` variables are undefined). You can run unit tests directly with `go test ./...` instead.

#### End-to-End Tests

Run the full end-to-end test suite, which creates a fresh kind cluster, installs Cluster API and the provider, applies a cluster template, and validates the entire lifecycle:

```sh
make e2e-test
```

You can also create a kind cluster for manual testing:

```sh
make kind-cluster
```

### Docker Build

```sh
make docker-build
make docker-push
```

### Make Targets

| Target | Description |
|--------|-------------|
| `make managers` | Build the provider binary |
| `make generate` | Run all code generation (manifests, deepcopy) |
| `make generate-manifests` | Generate CRD and RBAC manifests |
| `make generate-go-deepcopy` | Generate deepcopy functions |
| `make test` | Run unit and integration tests |
| `make e2e-test` | Run end-to-end tests |
| `make lint` | Lint the codebase |
| `make docker-build` | Build the Docker image |
| `make docker-push` | Push the Docker image |
| `make kind-cluster` | Create a kind cluster for local development |
| `make kind-load` | Build and load Docker image into kind |
| `make clean` | Remove generated binaries |

### Architecture

The provider implements all three Cluster API contracts:

- **Infrastructure Provider** (`KwokCluster`, `KwokMachine`, `KwokMachinePool`, `KwokMachineTemplate`) — manages simulated cluster infrastructure using KWOK runtimes (Docker Compose, kind, or binary) and simulated machine lifecycle
- **Control Plane Provider** (`KwokControlPlane`) — manages the simulated control plane lifecycle
- **Bootstrap Provider** (`KwokConfig`) — generates bootstrap data for simulated nodes

Since the kwok-controller cannot run inside the workload cluster with the kind runtime, the provider simulates kubelet heartbeats by periodically updating node conditions and coordinated leases on the workload cluster.

Controllers watch Cluster API `Cluster` resources and reconcile the corresponding KWOK resources to simulate cluster lifecycle operations.

#### Kustomize Deployment (alternative)

The provider can also be deployed via kustomize instead of Helm:

```sh
kustomize build config/default | kubectl apply -f -
```

## License

Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
