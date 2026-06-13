package policy

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
)

// Layer identifies which defense layer a violation belongs to.
type Layer string

const (
	LayerTaint         Layer = "Layer1-TaintToleration"
	LayerNodeAffinity  Layer = "Layer2-NodeAffinity"
	LayerPriorityClass Layer = "Layer3-PriorityClass"
	LayerResourceQuota Layer = "Layer4-ResourceQuota"
	LayerAdmission     Layer = "Layer5-Admission"
)

// Violation describes a policy violation with a human-readable message.
type Violation struct {
	Layer   Layer
	Message string
}

func (v Violation) String() string {
	return fmt.Sprintf("[%s] %s", v.Layer, v.Message)
}

// PodRequestsGPU returns true when any container requests or limits a GPU resource.
func PodRequestsGPU(pod *corev1.Pod, gpuResourceNames []string) bool {
	return GetRequestedGPUCount(pod, gpuResourceNames) > 0 ||
		hasGPULimitOnly(pod, gpuResourceNames)
}

func hasGPULimitOnly(pod *corev1.Pod, gpuResourceNames []string) bool {
	for _, c := range pod.Spec.Containers {
		for _, name := range gpuResourceNames {
			resName := corev1.ResourceName(name)
			if qty, ok := c.Resources.Limits[resName]; ok && !qty.IsZero() {
				if req, ok := c.Resources.Requests[resName]; !ok || req.IsZero() {
					return true
				}
			}
		}
	}
	return false
}

// GetRequestedGPUCount sums GPU requests across all containers.
func GetRequestedGPUCount(pod *corev1.Pod, gpuResourceNames []string) int64 {
	var total int64
	for _, c := range pod.Spec.Containers {
		for _, name := range gpuResourceNames {
			resName := corev1.ResourceName(name)
			if qty, ok := c.Resources.Requests[resName]; ok {
				total += qty.Value()
			}
		}
	}
	return total
}

// GetLimitedGPUCount sums GPU limits across all containers.
func GetLimitedGPUCount(pod *corev1.Pod, gpuResourceNames []string) int64 {
	var total int64
	for _, c := range pod.Spec.Containers {
		for _, name := range gpuResourceNames {
			resName := corev1.ResourceName(name)
			if qty, ok := c.Resources.Limits[resName]; ok {
				total += qty.Value()
			}
		}
	}
	return total
}

// NamespaceAllowed checks whether a namespace is authorized for GPU workloads.
func NamespaceAllowed(ns *corev1.Namespace, pol *platformv1alpha1.GPUIsolationPolicy) bool {
	if ns == nil || pol == nil {
		return false
	}
	for _, name := range pol.Spec.AllowedNamespaces.MatchNames {
		if ns.Name == name {
			return true
		}
	}
	if len(pol.Spec.AllowedNamespaces.MatchLabels) == 0 {
		return false
	}
	for k, v := range pol.Spec.AllowedNamespaces.MatchLabels {
		if ns.Labels[k] != v {
			return false
		}
	}
	return len(pol.Spec.AllowedNamespaces.MatchLabels) > 0
}

// HasRequiredToleration checks whether the Pod tolerates the policy GPU taint.
func HasRequiredToleration(pod *corev1.Pod, pol *platformv1alpha1.GPUIsolationPolicy) bool {
	taint := pol.Spec.GPUTaint
	effect := corev1.TaintEffect(taint.Effect)
	for _, tol := range pod.Spec.Tolerations {
		if tol.Key != taint.Key || tol.Effect != effect {
			continue
		}
		if tol.Operator == corev1.TolerationOpExists {
			return true
		}
		if tol.Operator == corev1.TolerationOpEqual && tol.Value == taint.Value {
			return true
		}
	}
	return false
}

// HasRequiredNodeAffinity checks required node affinity against gpuNodeSelector.
func HasRequiredNodeAffinity(pod *corev1.Pod, pol *platformv1alpha1.GPUIsolationPolicy) bool {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		return false
	}
	req := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if req == nil {
		return false
	}
	for _, term := range req.NodeSelectorTerms {
		if nodeSelectorTermMatchesGPUNodes(term, pol.Spec.GPUNodeSelector) {
			return true
		}
	}
	return false
}

func nodeSelectorTermMatchesGPUNodes(term corev1.NodeSelectorTerm, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	matched := map[string]bool{}
	for k := range selector {
		matched[k] = false
	}

	for _, expr := range term.MatchExpressions {
		expected, ok := selector[expr.Key]
		if !ok {
			continue
		}
		switch expr.Operator {
		case corev1.NodeSelectorOpIn:
			for _, v := range expr.Values {
				if v == expected {
					matched[expr.Key] = true
				}
			}
		case corev1.NodeSelectorOpExists:
			matched[expr.Key] = true
		}
	}

	for _, req := range term.MatchFields {
		expected, ok := selector[req.Key]
		if !ok {
			continue
		}
		if req.Operator == corev1.NodeSelectorOpIn {
			for _, v := range req.Values {
				if v == expected {
					matched[req.Key] = true
				}
			}
		}
	}

	for _, v := range matched {
		if !v {
			return false
		}
	}
	return true
}

// HasIllegalGPUNodeTargeting returns true when a non-GPU Pod targets GPU nodes.
func HasIllegalGPUNodeTargeting(pod *corev1.Pod, pol *platformv1alpha1.GPUIsolationPolicy) bool {
	if PodRequestsGPU(pod, pol.Spec.GPUResourceNames) {
		return false
	}
	return targetsGPUNodes(pod, pol)
}

func targetsGPUNodes(pod *corev1.Pod, pol *platformv1alpha1.GPUIsolationPolicy) bool {
	for k, v := range pol.Spec.GPUNodeSelector {
		if pod.Spec.NodeSelector != nil && pod.Spec.NodeSelector[k] == v {
			return true
		}
	}
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		req := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
		if req != nil {
			for _, term := range req.NodeSelectorTerms {
				if nodeSelectorTermMatchesGPUNodes(term, pol.Spec.GPUNodeSelector) {
					return true
				}
			}
		}
	}
	return false
}

// ValidateGPUPod validates a GPU-requesting Pod against the policy.
func ValidateGPUPod(pod *corev1.Pod, ns *corev1.Namespace, pol *platformv1alpha1.GPUIsolationPolicy) []Violation {
	if pol == nil {
		return []Violation{{Layer: LayerAdmission, Message: "no active GPUIsolationPolicy found"}}
	}

	var violations []Violation
	v := pol.Spec.Validation

	if v.DenyGpuInUnauthorizedNamespace && !NamespaceAllowed(ns, pol) {
		violations = append(violations, Violation{
			Layer:   LayerResourceQuota,
			Message: fmt.Sprintf("namespace %q is not authorized to request GPU resources", ns.Name),
		})
	}

	if v.DenyMissingPriorityClass {
		if pod.Spec.PriorityClassName == "" {
			violations = append(violations, Violation{
				Layer:   LayerPriorityClass,
				Message: fmt.Sprintf("GPU Pod must use PriorityClass %q", pol.Spec.PriorityClass.Name),
			})
		} else if pod.Spec.PriorityClassName != pol.Spec.PriorityClass.Name {
			violations = append(violations, Violation{
				Layer:   LayerPriorityClass,
				Message: fmt.Sprintf("GPU Pod must use PriorityClass %q, got %q", pol.Spec.PriorityClass.Name, pod.Spec.PriorityClassName),
			})
		}
	}

	if v.DenyMissingToleration && !HasRequiredToleration(pod, pol) {
		t := pol.Spec.GPUTaint
		violations = append(violations, Violation{
			Layer: LayerTaint,
			Message: fmt.Sprintf("GPU Pod must tolerate taint %s=%s:%s", t.Key, t.Value, t.Effect),
		})
	}

	if v.DenyMissingNodeAffinity && !HasRequiredNodeAffinity(pod, pol) {
		violations = append(violations, Violation{
			Layer:   LayerNodeAffinity,
			Message: fmt.Sprintf("GPU Pod must declare required nodeAffinity matching selector %v", pol.Spec.GPUNodeSelector),
		})
	}

	if v.DenyNodeNameBypass && pod.Spec.NodeName != "" {
		violations = append(violations, Violation{
			Layer:   LayerAdmission,
			Message: "GPU Pod must not set spec.nodeName (scheduler bypass)",
		})
	}

	if v.DenyHostNetworkBypass && pod.Spec.HostNetwork {
		violations = append(violations, Violation{
			Layer:   LayerAdmission,
			Message: "GPU Pod must not enable hostNetwork (isolation bypass)",
		})
	}

	if v.DenyUnauthorizedSchedulerName {
		scheduler := pod.Spec.SchedulerName
		if scheduler != "" && scheduler != platformv1alpha1.DefaultAllowedScheduler {
			violations = append(violations, Violation{
				Layer:   LayerAdmission,
				Message: fmt.Sprintf("GPU Pod must use scheduler %q, got %q", platformv1alpha1.DefaultAllowedScheduler, scheduler),
			})
		}
	}

	violations = append(violations, validateGPUResources(pod, pol)...)

	if v.DenyIllegalGpuNodeAffinity && HasIllegalGPUNodeTargeting(pod, pol) {
		violations = append(violations, Violation{
			Layer:   LayerNodeAffinity,
			Message: "non-GPU Pod must not target GPU nodes via nodeSelector or nodeAffinity",
		})
	}

	return violations
}

func validateGPUResources(pod *corev1.Pod, pol *platformv1alpha1.GPUIsolationPolicy) []Violation {
	var violations []Violation
	requested := GetRequestedGPUCount(pod, pol.Spec.GPUResourceNames)
	limited := GetLimitedGPUCount(pod, pol.Spec.GPUResourceNames)

	if requested <= 0 {
		violations = append(violations, Violation{
			Layer:   LayerAdmission,
			Message: "GPU Pod must request a positive integer GPU count",
		})
	}

	for _, c := range pod.Spec.Containers {
		for _, name := range pol.Spec.GPUResourceNames {
			resName := corev1.ResourceName(name)
			req, hasReq := c.Resources.Requests[resName]
			lim, hasLim := c.Resources.Limits[resName]
			if !hasReq && !hasLim {
				continue
			}
			if hasReq {
				if req.Value() <= 0 {
					violations = append(violations, Violation{
						Layer:   LayerAdmission,
						Message: fmt.Sprintf("container %q GPU request for %s must be a positive integer", c.Name, name),
					})
				}
			}
			if hasReq && hasLim && req.Cmp(lim) != 0 {
				violations = append(violations, Violation{
					Layer:   LayerAdmission,
					Message: fmt.Sprintf("container %q GPU request and limit for %s must match (request=%s limit=%s)", c.Name, name, req.String(), lim.String()),
				})
			}
			if hasLim && !hasReq {
				violations = append(violations, Violation{
					Layer:   LayerAdmission,
					Message: fmt.Sprintf("container %q must set GPU request when limit is set for %s", c.Name, name),
				})
			}
		}
	}

	if requested > 0 && limited > 0 && requested != limited {
		violations = append(violations, Violation{
			Layer:   LayerAdmission,
			Message: fmt.Sprintf("Pod GPU request total (%d) must equal limit total (%d)", requested, limited),
		})
	}

	return violations
}

// MutateGPUPod injects required fields for authorized GPU Pods. Returns true if changed.
func MutateGPUPod(pod *corev1.Pod, pol *platformv1alpha1.GPUIsolationPolicy) bool {
	if pol == nil || !PodRequestsGPU(pod, pol.Spec.GPUResourceNames) {
		return false
	}

	changed := false
	m := pol.Spec.Mutation

	if m.InjectToleration && !HasRequiredToleration(pod, pol) {
		pod.Spec.Tolerations = append(pod.Spec.Tolerations, BuildToleration(pol))
		changed = true
	}

	if m.InjectNodeAffinity && !HasRequiredNodeAffinity(pod, pol) {
		affinity := BuildNodeAffinity(pol)
		if pod.Spec.Affinity == nil {
			pod.Spec.Affinity = &corev1.Affinity{}
		}
		pod.Spec.Affinity.NodeAffinity = affinity
		changed = true
	}

	if m.InjectPriorityClass && pod.Spec.PriorityClassName == "" {
		pod.Spec.PriorityClassName = pol.Spec.PriorityClass.Name
		changed = true
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[platformv1alpha1.AnnotationPolicyName] = pol.Name
	if changed {
		pod.Annotations[platformv1alpha1.AnnotationPolicyMutated] = "true"
	}

	return changed
}

// BuildToleration creates a toleration from policy spec.
func BuildToleration(pol *platformv1alpha1.GPUIsolationPolicy) corev1.Toleration {
	t := pol.Spec.GPUTaint
	return corev1.Toleration{
		Key:      t.Key,
		Value:    t.Value,
		Effect:   corev1.TaintEffect(t.Effect),
		Operator: corev1.TolerationOpEqual,
	}
}

// BuildNodeAffinity creates required node affinity from gpuNodeSelector.
func BuildNodeAffinity(pol *platformv1alpha1.GPUIsolationPolicy) *corev1.NodeAffinity {
	expressions := make([]corev1.NodeSelectorRequirement, 0, len(pol.Spec.GPUNodeSelector))
	keys := make([]string, 0, len(pol.Spec.GPUNodeSelector))
	for k := range pol.Spec.GPUNodeSelector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		expressions = append(expressions, corev1.NodeSelectorRequirement{
			Key:      k,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{pol.Spec.GPUNodeSelector[k]},
		})
	}
	return &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: expressions},
			},
		},
	}
}

// ResourceQuotaNameForNamespace returns the operator-managed quota name.
func ResourceQuotaNameForNamespace(policyName, namespace string) string {
	return fmt.Sprintf("gpu-isolation-%s-%s", policyName, namespace)
}

// BuildAllowedResourceQuota creates a quota allowing GPU up to the configured limit.
func BuildAllowedResourceQuota(policyName, namespace string, pol *platformv1alpha1.GPUIsolationPolicy) *corev1.ResourceQuota {
	hard := corev1.ResourceList{}
	limit := pol.Spec.Quota.AllowedNamespaceGpuLimit
	if limit == "" {
		limit = "8"
	}
	for _, res := range pol.Spec.GPUResourceNames {
		qty := resource.MustParse(limit)
		hard[corev1.ResourceName("requests."+res)] = qty
		hard[corev1.ResourceName("limits."+res)] = qty
	}
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceQuotaNameForNamespace(policyName, namespace),
			Namespace: namespace,
			Labels: map[string]string{
				platformv1alpha1.LabelManagedBy: "gpu-isolation-operator",
				"platform.example.com/policy": policyName,
			},
		},
		Spec: corev1.ResourceQuotaSpec{Hard: hard},
	}
}

// BuildDenyResourceQuota creates a quota that blocks GPU requests in a namespace.
func BuildDenyResourceQuota(policyName, namespace string, pol *platformv1alpha1.GPUIsolationPolicy) *corev1.ResourceQuota {
	hard := corev1.ResourceList{}
	zero := resource.MustParse("0")
	for _, res := range pol.Spec.GPUResourceNames {
		hard[corev1.ResourceName("requests."+res)] = zero
		hard[corev1.ResourceName("limits."+res)] = zero
	}
	return &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResourceQuotaNameForNamespace(policyName, namespace),
			Namespace: namespace,
			Labels: map[string]string{
				platformv1alpha1.LabelManagedBy: "gpu-isolation-operator",
				"platform.example.com/policy":   policyName,
				"platform.example.com/gpu-deny":   "true",
			},
		},
		Spec: corev1.ResourceQuotaSpec{Hard: hard},
	}
}

// NodeMatchesGPUNodeSelector checks whether a node matches the GPU selector.
func NodeMatchesGPUNodeSelector(node *corev1.Node, selector map[string]string) bool {
	if node == nil {
		return false
	}
	for k, v := range selector {
		if node.Labels[k] != v {
			return false
		}
	}
	return len(selector) > 0
}

// NodeHasGPUTaint checks whether a node already has the policy taint.
func NodeHasGPUTaint(node *corev1.Node, pol *platformv1alpha1.GPUIsolationPolicy) bool {
	t := pol.Spec.GPUTaint
	effect := corev1.TaintEffect(t.Effect)
	for _, existing := range node.Spec.Taints {
		if existing.Key == t.Key && existing.Value == t.Value && existing.Effect == effect {
			return true
		}
	}
	return false
}

// BuildGPUTaint returns the corev1.Taint for the policy.
func BuildGPUTaint(pol *platformv1alpha1.GPUIsolationPolicy) corev1.Taint {
	t := pol.Spec.GPUTaint
	return corev1.Taint{
		Key:    t.Key,
		Value:  t.Value,
		Effect: corev1.TaintEffect(t.Effect),
	}
}

// FormatViolations joins violations for annotations and error messages.
func FormatViolations(violations []Violation) string {
	msgs := make([]string, 0, len(violations))
	for _, v := range violations {
		msgs = append(msgs, v.String())
	}
	return strings.Join(msgs, "; ")
}

// IsEnforceMode returns true when the policy is in Enforce mode.
func IsEnforceMode(pol *platformv1alpha1.GPUIsolationPolicy) bool {
	if pol == nil {
		return true
	}
	return pol.Spec.EnforcementMode != platformv1alpha1.EnforcementAudit
}

// ParsePositiveInt parses a quota limit string.
func ParsePositiveInt(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
