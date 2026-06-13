# GPU Isolation Operator

**语言 / Languages:** [English](README.md) · [简体中文](README_zh-CN.md)

基于 **Go + Kubebuilder / controller-runtime** 的 Kubernetes Operator，通过 `GPUIsolationPolicy` CRD 实现 GPU 资源的**五层防护**隔离与准入控制。

## 架构概览

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

## 五层防护模型

| 层级 | 机制 | Operator 职责 |
|------|------|---------------|
| **第1层** | Taint + Toleration | Controller 为匹配 `gpuNodeSelector` 的 Node 打上 `gpuTaint`；Webhook 校验/注入 Toleration |
| **第2层** | NodeAffinity | Mutating Webhook 为合法 GPU Pod 注入 `requiredDuringSchedulingIgnoredDuringExecution`；非法 affinity 由 Validating Webhook 拒绝 |
| **第3层** | PriorityClass | Controller 创建/更新 GPU 专用 PriorityClass；Webhook 校验 Pod 必须使用指定 PriorityClass |
| **第4层** | ResourceQuota | Controller 为授权/未授权 Namespace 分别创建允许/禁止 GPU 的 ResourceQuota（`requests.nvidia.com/gpu`） |
| **第5层** | Admission Webhook | 最终拦截：校验 namespace、PriorityClass、toleration、affinity、request/limit、nodeName/hostNetwork/scheduler 绕过 |

## 项目结构

```
.
├── api/v1alpha1/
│   ├── gpuisolationpolicy_types.go   # CRD 类型定义
│   └── groupversion_info.go
├── controllers/
│   └── gpuisolationpolicy_controller.go
├── webhooks/
│   ├── pod_mutating_webhook.go
│   └── pod_validating_webhook.go
├── internal/policy/
│   ├── policy.go                     # 策略判断与 mutation 核心逻辑
│   └── pod_checker.go                # Active policy 解析、非 GPU Pod 校验
├── config/
│   ├── crd/bases/                    # CRD manifest
│   ├── rbac/                         # ServiceAccount / ClusterRole / Binding
│   ├── webhook/                      # Webhook Service + 配置
│   ├── certmanager/                  # cert-manager 证书
│   ├── manager/                      # Deployment
│   └── samples/                      # 示例 CR / Pod
├── helm/gpu-isolation-operator/      # Helm Chart
├── main.go
├── Dockerfile
└── Makefile
```

## Active Policy 策略

集群中可能存在多个 `GPUIsolationPolicy`，但**同一时刻仅一个生效**：

1. 优先选择带有 `platform.example.com/active: "true"` 标签的 CR（有且只能有一个）
2. 若无标签，且集群中仅有一个 CR，则自动使用该 CR
3. 若有多个未标记 CR，优先使用名为 `default-gpu-policy` 的 CR
4. 否则 Controller / Webhook 报错，提示为其中一个 CR 打上 active 标签

## 安装

### 方式 A：Helm Chart（推荐）

```bash
# 校验 Chart
helm lint helm/gpu-isolation-operator

# 预览渲染结果
helm template gpu-isolation helm/gpu-isolation-operator --namespace gpu-isolation-system

# 安装（需集群已安装 cert-manager）
helm upgrade --install gpu-isolation helm/gpu-isolation-operator \
  --namespace gpu-isolation-system --create-namespace

# 开发环境：同时安装默认 GPUIsolationPolicy
helm upgrade --install gpu-isolation helm/gpu-isolation-operator \
  --namespace gpu-isolation-system --create-namespace \
  -f helm/gpu-isolation-operator/values-dev.yaml

# 自定义镜像
helm upgrade --install gpu-isolation helm/gpu-isolation-operator \
  --namespace gpu-isolation-system --create-namespace \
  --set image.repository=your-registry/gpu-isolation-operator \
  --set image.tag=v0.1.0 \
  --set defaultPolicy.enabled=true
```

Helm Chart 位于 `helm/gpu-isolation-operator/`，主要 values：

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `namespace.name` | Operator 命名空间 | `gpu-isolation-system` |
| `image.repository` / `image.tag` | 镜像 | `gpu-isolation-operator:0.1.0` |
| `certManager.enabled` | 使用 cert-manager 签发 Webhook 证书 | `true` |
| `defaultPolicy.enabled` | 安装默认 GPUIsolationPolicy | `false` |
| `webhook.caBundle` | cert-manager 关闭时手动注入 CA | `""` |
| `leaderElection.enabled` | 控制器选主 | `true` |

CRD 位于 Chart 的 `crds/` 目录，Helm 会在首次安装时自动应用（upgrade 时不自动更新 CRD，需手动 apply）。

Makefile 快捷命令：

```bash
make helm-lint
make helm-template
make helm-install
make helm-install-dev
make helm-uninstall
```

### 方式 B：kubectl 直接部署

**前置条件：**

- Kubernetes 1.28+
- kubectl
- cert-manager（生产环境推荐，用于 Webhook TLS）
- （可选）NVIDIA Device Plugin / GPU 节点

**构建与部署：**

```bash
# 生成 deepcopy / CRD
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/v1alpha1/..."
controller-gen crd paths="./api/v1alpha1/..." output:crd:artifacts:config=config/crd/bases

# 编译
go build -o bin/manager main.go

# 构建镜像
docker build -t gpu-isolation-operator:latest .

# 部署（需先安装 cert-manager）
kubectl apply -f config/crd/bases/
kubectl apply -f config/rbac/service_account.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/rbac/role_binding.yaml
kubectl apply -f config/certmanager/
kubectl apply -f config/webhook/service.yaml
kubectl apply -f config/manager/manager.yaml
kubectl apply -f config/webhook/manifests.yaml
```

### 开发模式（不依赖 cert-manager）

```bash
# 1. 生成本地自签证书
openssl req -x509 -newkey rsa:4096 -keyout tls.key -out tls.crt -days 365 -nodes \
  -subj "/CN=gpu-isolation-webhook-service.gpu-isolation-system.svc"

# 2. 创建 Secret
kubectl create namespace gpu-isolation-system --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret tls gpu-isolation-webhook-server-cert \
  --cert=tls.crt --key=tls.key -n gpu-isolation-system

# 3. 将 caBundle 写入 WebhookConfiguration（base64 编码 tls.crt）
# 4. 移除 manifests.yaml 中 cert-manager.io/inject-ca-from 注解
# 5. 本地运行 manager
go run main.go --webhook-cert-dir=. \
  --metrics-bind-address=127.0.0.1:8080 \
  --health-probe-bind-address=:8081
```

## 使用方式

### 1. 创建策略

```bash
kubectl apply -f config/samples/platform_v1alpha1_gpuisolationpolicy.yaml
kubectl apply -f config/samples/pod_samples.yaml
```

### 2. 标记 GPU 节点

```bash
kubectl label node <gpu-node> accelerator=nvidia-gpu
```

Controller 会自动为该节点添加 taint `dedicated=gpu:NoSchedule`。

### 3. 提交合法 GPU Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-gpu-job
  namespace: ml-platform
spec:
  containers:
    - name: trainer
      image: nvcr.io/nvidia/pytorch:24.01-py3
      resources:
        requests:
          nvidia.com/gpu: "1"
        limits:
          nvidia.com/gpu: "1"
```

Mutating Webhook 会自动注入 toleration、nodeAffinity、priorityClassName，并添加 annotation：

- `platform.example.com/gpu-policy: default-gpu-policy`
- `platform.example.com/gpu-policy-mutated: "true"`

### 4. Audit 模式

将 `spec.enforcementMode` 设为 `Audit`：非法 Pod **不会被拒绝**，但 Validating Webhook 会返回 warning，Mutating Webhook 仅写入 violation annotation。

## 安全边界

- GPU 资源名从 `spec.gpuResourceNames` 读取，**不硬编码** `nvidia.com/gpu`
- Validating Webhook 拦截 `nodeName`、`hostNetwork`、非默认 `schedulerName` 等绕过手段
- 非 GPU Pod 若手动指定 GPU 节点 affinity，也会被拒绝（`denyIllegalGpuNodeAffinity`）
- ResourceQuota 在 API Server 层限制未授权 namespace 的 GPU 申请
- Webhook `failurePolicy: Fail`，确保 API Server 无法静默绕过

## 已知限制

- Controller 目前**不会移除**已不匹配 `gpuNodeSelector` 的旧 taint（避免误伤）
- ResourceQuota 对所有非系统 namespace 生效，大规模集群 reconcile 可能较慢
- 仅支持 extended resource 型 GPU（如 `nvidia.com/gpu`），不支持 GPU 分片 / MIG 细粒度策略
- 多 Policy 并存时，非 active Policy 的 Controller 会进入 NotReady 状态
- Windows 开发环境下 envtest 需要手动下载 kubebuilder assets

## 验证 Webhook 生效

```bash
kubectl get mutatingwebhookconfiguration
kubectl get validatingwebhookconfiguration

# 非法 Pod（default namespace）应被拒绝
kubectl apply -f config/samples/pod_samples.yaml

# 合法 Pod 应被 mutation
kubectl get pod -n ml-platform -o yaml | grep platform.example.com
```

## 测试

```bash
# 单元测试
go test ./internal/policy/... ./webhooks/... -v

# Controller envtest（需 kubebuilder assets）
make envtest
export KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.31.0 -p path)"
go test ./controllers/... -v -timeout 5m
```

测试覆盖场景：

- Pod 不申请 GPU → 允许
- 未授权 namespace 申请 GPU → 拒绝
- 授权 namespace + mutation 后 → 允许
- 缺少 toleration / nodeAffinity → mutation 注入或 validation 拒绝
- PriorityClass 错误 → 拒绝
- nodeName 绕过 → 拒绝
- request/limit 不一致 → 拒绝
- Audit 模式 → 允许 + warning

## API 参考

```yaml
apiVersion: platform.example.com/v1alpha1
kind: GPUIsolationPolicy
metadata:
  name: default-gpu-policy
  labels:
    platform.example.com/active: "true"
spec:
  enforcementMode: Enforce   # Enforce | Audit
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

## License

Apache License 2.0
