package policy

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
)

func samplePolicy() *platformv1alpha1.GPUIsolationPolicy {
	return &platformv1alpha1.GPUIsolationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default-gpu-policy"},
		Spec: platformv1alpha1.GPUIsolationPolicySpec{
			EnforcementMode: platformv1alpha1.EnforcementEnforce,
			GPUResourceNames: []string{"nvidia.com/gpu"},
			GPUNodeSelector:  map[string]string{"accelerator": "nvidia-gpu"},
			GPUTaint: platformv1alpha1.TaintSpec{
				Key: "dedicated", Value: "gpu", Effect: string(corev1.TaintEffectNoSchedule),
			},
			AllowedNamespaces: platformv1alpha1.NamespaceSelector{
				MatchNames: []string{"ml-platform", "ai-training"},
			},
			PriorityClass: platformv1alpha1.PriorityClassSpec{Name: "gpu-high-priority", Value: 100000},
			Mutation: platformv1alpha1.MutationSpec{
				InjectToleration: true, InjectNodeAffinity: true, InjectPriorityClass: true,
			},
			Validation: platformv1alpha1.ValidationSpec{
				DenyGpuInUnauthorizedNamespace: true,
				DenyMissingPriorityClass:       true,
				DenyMissingToleration:          true,
				DenyMissingNodeAffinity:        true,
				DenyNodeNameBypass:             true,
				DenyUnauthorizedSchedulerName:  true,
				DenyIllegalGpuNodeAffinity:     true,
			},
		},
	}
}

func gpuPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-job", Namespace: "ml-platform"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "worker",
				Image: "nvidia/cuda:12.0-base",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			}},
		},
	}
}

func TestPodRequestsGPU_NoGPU(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx"}}}}
	if PodRequestsGPU(pod, []string{"nvidia.com/gpu"}) {
		t.Fatal("expected no GPU request")
	}
}

func TestNamespaceAllowed(t *testing.T) {
	pol := samplePolicy()
	allowed := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}
	denied := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	if !NamespaceAllowed(allowed, pol) {
		t.Fatal("ml-platform should be allowed")
	}
	if NamespaceAllowed(denied, pol) {
		t.Fatal("default should be denied")
	}
}

func TestMutateGPUPod_InjectsFields(t *testing.T) {
	pol := samplePolicy()
	pod := gpuPod()
	if !MutateGPUPod(pod, pol) {
		t.Fatal("expected mutation")
	}
	if !HasRequiredToleration(pod, pol) {
		t.Fatal("missing toleration after mutation")
	}
	if !HasRequiredNodeAffinity(pod, pol) {
		t.Fatal("missing nodeAffinity after mutation")
	}
	if pod.Spec.PriorityClassName != pol.Spec.PriorityClass.Name {
		t.Fatalf("priorityClassName = %q", pod.Spec.PriorityClassName)
	}
}

func TestValidateGPUPod_UnauthorizedNamespace(t *testing.T) {
	pol := samplePolicy()
	pod := gpuPod()
	pod.Namespace = "default"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	v := ValidateGPUPod(pod, ns, pol)
	if len(v) == 0 {
		t.Fatal("expected violations")
	}
}

func TestValidateGPUPod_MissingFields(t *testing.T) {
	pol := samplePolicy()
	pod := gpuPod()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}
	v := ValidateGPUPod(pod, ns, pol)
	if len(v) < 3 {
		t.Fatalf("expected at least 3 violations, got %d: %v", len(v), v)
	}
}

func TestValidateGPUPod_CompliantAfterMutation(t *testing.T) {
	pol := samplePolicy()
	pod := gpuPod()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}
	MutateGPUPod(pod, pol)
	v := ValidateGPUPod(pod, ns, pol)
	if len(v) != 0 {
		t.Fatalf("expected no violations, got %v", v)
	}
}

func TestValidateGPUPod_NodeNameBypass(t *testing.T) {
	pol := samplePolicy()
	pod := gpuPod()
	MutateGPUPod(pod, pol)
	pod.Spec.NodeName = "gpu-node-1"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}
	v := ValidateGPUPod(pod, ns, pol)
	found := false
	for _, item := range v {
		if item.Layer == LayerAdmission {
			found = true
		}
	}
	if !found {
		t.Fatal("expected nodeName bypass violation")
	}
}

func TestValidateGPUPod_RequestLimitMismatch(t *testing.T) {
	pol := samplePolicy()
	pod := gpuPod()
	pod.Spec.Containers[0].Resources.Limits[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("2")
	MutateGPUPod(pod, pol)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}
	v := ValidateGPUPod(pod, ns, pol)
	if len(v) == 0 {
		t.Fatal("expected request/limit mismatch violation")
	}
}

func TestValidateGPUPod_WrongPriorityClass(t *testing.T) {
	pol := samplePolicy()
	pod := gpuPod()
	MutateGPUPod(pod, pol)
	pod.Spec.PriorityClassName = "system-cluster-critical"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}
	v := ValidateGPUPod(pod, ns, pol)
	if len(v) == 0 {
		t.Fatal("expected priority class violation")
	}
}

func TestIsEnforceMode_Audit(t *testing.T) {
	pol := samplePolicy()
	pol.Spec.EnforcementMode = platformv1alpha1.EnforcementAudit
	if IsEnforceMode(pol) {
		t.Fatal("audit mode should not enforce")
	}
}

func TestBuildResourceQuotas(t *testing.T) {
	pol := samplePolicy()
	pol.Spec.Quota.AllowedNamespaceGpuLimit = "8"
	allowed := BuildAllowedResourceQuota(pol.Name, "ml-platform", pol)
	allowedQty := allowed.Spec.Hard[corev1.ResourceName("requests.nvidia.com/gpu")]
	if allowedQty.Cmp(resource.MustParse("8")) != 0 {
		t.Fatalf("unexpected allowed quota: %v", allowed.Spec.Hard)
	}
	deny := BuildDenyResourceQuota(pol.Name, "default", pol)
	denyQty := deny.Spec.Hard[corev1.ResourceName("requests.nvidia.com/gpu")]
	if denyQty.Cmp(resource.MustParse("0")) != 0 {
		t.Fatalf("unexpected deny quota: %v", deny.Spec.Hard)
	}
}
