# GPU Isolation Operator

**Languages:** [English](README.md) · [简体中文](README_zh-CN.md)

A **Go + Kubebuilder / controller-runtime** Kubernetes Operator that enforces GPU resource isolation and admission control through the `GPUIsolationPolicy` CRD — a **five-layer defense** model.

## Highlights

- **Layer 1–5 enforcement**: Taint/Toleration, NodeAffinity, PriorityClass, ResourceQuota, Admission Webhooks
- **Declarative policy** via cluster-scoped `GPUIsolationPolicy` CRD
- **Mutating + Validating** webhooks with shared `internal/policy` logic
- **Helm Chart** included (`helm/gpu-isolation-operator/`)
- **Enforce** or **Audit** mode

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    GPUIsolationPolicy (CRD)                      │
│  spec: taint / affinity / priority / quota / webhook rules       │
└───────────────────────────┬─────────────────────────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         ▼                  ▼                  ▼
  ┌─────────────┐   ┌──────────────┐   ┌───────────────────┐
  │ Controller  │   │ Mutating WH  │   │ Validating WH     │
  │ Reconciler  │   │ (Pod CREATE) │   │ (Pod CREATE/UPD)  │
  └──────┬──────┘   └──────┬───────┘   └─────────┬─────────┘
         │                 │                      │
         ▼                 └──────────┬───────────┘
  PriorityClass                      ▼
  Node Taints                 internal/policy
  ResourceQuota                 (shared logic)
```

## Five-Layer Defense Model

| Layer | Mechanism | Operator responsibility |
|-------|-----------|-------------------------|
| **L1** | Taint + Toleration | Controller taints GPU nodes; webhook validates/injects tolerations |
| **L2** | NodeAffinity | Mutating webhook injects required affinity; validating webhook rejects illegal targeting |
| **L3** | PriorityClass | Controller manages GPU PriorityClass; webhook enforces correct usage |
| **L4** | ResourceQuota | Controller creates allow/deny quotas per namespace |
| **L5** | Admission Webhook | Final gate: namespace, priority, toleration, affinity, resources, bypass checks |

## Project Layout

```
.
├── api/v1alpha1/              # CRD types
├── controllers/               # GPUIsolationPolicy reconciler
├── webhooks/                  # Pod mutating & validating webhooks
├── internal/policy/           # Shared policy engine
├── config/                    # Kustomize-style manifests
├── helm/gpu-isolation-operator/
├── main.go
├── Dockerfile
└── Makefile
```

## Quick Start (Helm)

```bash
# Lint chart
helm lint helm/gpu-isolation-operator

# Install (requires cert-manager)
helm upgrade --install gpu-isolation helm/gpu-isolation-operator \
  --namespace gpu-isolation-system --create-namespace

# Dev install with default policy
helm upgrade --install gpu-isolation helm/gpu-isolation-operator \
  --namespace gpu-isolation-system --create-namespace \
  -f helm/gpu-isolation-operator/values-dev.yaml
```

## Prerequisites

- Kubernetes 1.28+
- [cert-manager](https://cert-manager.io/) (recommended for webhook TLS)
- NVIDIA Device Plugin / GPU nodes (optional, for real GPU workloads)

## Documentation

| Topic | English | 中文 |
|-------|---------|------|
| Full guide | This file | [README_zh-CN.md](README_zh-CN.md) |
| Helm values | `helm/gpu-isolation-operator/values.yaml` | same |
| Sample policy | `config/samples/platform_v1alpha1_gpuisolationpolicy.yaml` | same |

## Build & Test

```bash
go build -o bin/manager main.go
go test ./internal/policy/... ./webhooks/... -v
make helm-lint
```

## Active Policy Selection

Only one `GPUIsolationPolicy` is active at a time:

1. CR labeled `platform.example.com/active: "true"` (exactly one)
2. If unlabeled and only one CR exists → use it
3. If multiple unlabeled → prefer `default-gpu-policy`
4. Otherwise → controller/webhook reports an error

## Example Policy

```yaml
apiVersion: platform.example.com/v1alpha1
kind: GPUIsolationPolicy
metadata:
  name: default-gpu-policy
  labels:
    platform.example.com/active: "true"
spec:
  enforcementMode: Enforce
  gpuResourceNames: [nvidia.com/gpu]
  gpuNodeSelector: { accelerator: nvidia-gpu }
  gpuTaint: { key: dedicated, value: gpu, effect: NoSchedule }
  allowedNamespaces:
    matchNames: [ml-platform, ai-training]
    matchLabels: { gpu-access: "true" }
  priorityClass:
    name: gpu-high-priority
    value: 100000
  quota:
    defaultDenyGpu: true
    allowedNamespaceGpuLimit: "8"
  mutation:
    injectToleration: true
    injectNodeAffinity: true
    injectPriorityClass: true
  validation:
    denyGpuInUnauthorizedNamespace: true
    denyMissingPriorityClass: true
    denyMissingToleration: true
    denyMissingNodeAffinity: true
    denyNodeNameBypass: true
    denyUnauthorizedSchedulerName: true
```

## Security Boundaries

- GPU resource names read from `spec.gpuResourceNames` — no hardcoded `nvidia.com/gpu`
- Blocks `nodeName`, `hostNetwork`, and unauthorized `schedulerName` bypass
- Rejects non-GPU pods targeting GPU nodes via affinity
- Webhook `failurePolicy: Fail`

## Known Limitations

- Does not remove stale taints from nodes that no longer match `gpuNodeSelector`
- ResourceQuota reconciled for all non-system namespaces (may be slow at scale)
- Extended GPU resources only; no MIG / fractional GPU support
- CRD updates via Helm require manual `kubectl apply` on upgrade

## License

Apache License 2.0
