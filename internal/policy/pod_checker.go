package policy

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ActivePolicyResult holds the resolved active policy or an error.
type ActivePolicyResult struct {
	Policy *platformv1alpha1.GPUIsolationPolicy
	Error  error
}

// ResolveActivePolicy selects the single active GPUIsolationPolicy.
// Selection order:
//  1. Exactly one CR labeled platform.example.com/active=true
//  2. If none labeled, exactly one CR in the cluster
//  3. If multiple unlabeled, prefer metadata.name=default-gpu-policy
//  4. Otherwise return an error
func ResolveActivePolicy(ctx context.Context, c client.Client) (*platformv1alpha1.GPUIsolationPolicy, error) {
	var list platformv1alpha1.GPUIsolationPolicyList
	if err := c.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list GPUIsolationPolicy: %w", err)
	}

	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no GPUIsolationPolicy found")
	}

	var active []platformv1alpha1.GPUIsolationPolicy
	for _, item := range list.Items {
		if item.Labels != nil && item.Labels[platformv1alpha1.LabelActivePolicy] == "true" {
			active = append(active, item)
		}
	}

	switch {
	case len(active) == 1:
		p := active[0]
		return &p, nil
	case len(active) > 1:
		return nil, fmt.Errorf("multiple GPUIsolationPolicy resources labeled %s=true; only one may be active", platformv1alpha1.LabelActivePolicy)
	case len(list.Items) == 1:
		p := list.Items[0]
		return &p, nil
	default:
		for i := range list.Items {
			if list.Items[i].Name == "default-gpu-policy" {
				p := list.Items[i]
				return &p, nil
			}
		}
		return nil, fmt.Errorf("multiple GPUIsolationPolicy resources found; label one with %s=true", platformv1alpha1.LabelActivePolicy)
	}
}

// ValidateNonGPUPod checks pods that do not request GPU but may illegally target GPU nodes.
func ValidateNonGPUPod(pod *corev1.Pod, ns *corev1.Namespace, pol *platformv1alpha1.GPUIsolationPolicy) []Violation {
	if pol == nil || PodRequestsGPU(pod, pol.Spec.GPUResourceNames) {
		return nil
	}

	var violations []Violation
	v := pol.Spec.Validation

	if v.DenyIllegalGpuNodeAffinity && HasIllegalGPUNodeTargeting(pod, pol) {
		violations = append(violations, Violation{
			Layer:   LayerNodeAffinity,
			Message: "non-GPU Pod must not target GPU nodes via nodeSelector or nodeAffinity",
		})
	}

	if v.DenyGpuInUnauthorizedNamespace && targetsGPUNodes(pod, pol) && !NamespaceAllowed(ns, pol) {
		violations = append(violations, Violation{
			Layer:   LayerNodeAffinity,
			Message: fmt.Sprintf("namespace %q is not authorized to schedule onto GPU nodes", ns.Name),
		})
	}

	return violations
}
