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
