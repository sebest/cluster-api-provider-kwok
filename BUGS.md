# Known Bugs and Workarounds

## 1. Docker Runtime: Bind Mount Race Condition

**Status:** Upstream bug in KWOK library (cannot fix from this provider)

**Runtime:** `docker` (Docker Compose)

**Symptom:** kube-apiserver container crashes on startup with errors about PKI
certificate files being directories instead of files.

**Root cause:** The KWOK Docker Compose runtime creates containers with bind
mounts to PKI certificate/key files _before_ those files have been generated.
Docker sees that the source path doesn't exist and creates a directory
placeholder. When KWOK subsequently generates the actual certificate files, the
bind mount is already pointing to a directory, and kube-apiserver fails to read
them.

**Workaround:** Use the `binary` runtime instead. Set `spec.runtime: binary` on
the KwokCluster resource.

---

## 2. KWOK Library: Context Warnings

**Status:** Upstream cosmetic issue in KWOK library

**Symptom:** Log warnings like:
```
Unable to get from context: ...
Unable to add to context: ...
```

**Root cause:** The KWOK library's internal configuration context is not fully
initialized when certain operations are performed. These warnings are cosmetic
and do not affect functionality.

**Workaround:** None needed. The warnings can be safely ignored.

---

## 3. Cluster API v1beta1 Deprecation

**Status:** Technical debt

**Symptom:** All Cluster API imports use the deprecated
`sigs.k8s.io/cluster-api/api/v1beta1` package.

**Impact:** Future versions of Cluster API will remove v1beta1. The provider
should be migrated to the current stable API version when upgrading dependencies.

**References:**
- `sigs.k8s.io/cluster-api/api/v1beta1` (deprecated)
- `sigs.k8s.io/cluster-api/exp/api/v1beta1` (deprecated)
