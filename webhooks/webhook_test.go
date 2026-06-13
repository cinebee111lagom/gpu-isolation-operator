package webhooks

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = platformv1alpha1.AddToScheme(s)
	return s
}

func testPolicy() *platformv1alpha1.GPUIsolationPolicy {
	return &platformv1alpha1.GPUIsolationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "default-gpu-policy",
			Labels: map[string]string{platformv1alpha1.LabelActivePolicy: "true"},
		},
		Spec: platformv1alpha1.GPUIsolationPolicySpec{
			EnforcementMode:  platformv1alpha1.EnforcementEnforce,
			GPUResourceNames: []string{"nvidia.com/gpu"},
			GPUNodeSelector:  map[string]string{"accelerator": "nvidia-gpu"},
			GPUTaint:         platformv1alpha1.TaintSpec{Key: "dedicated", Value: "gpu", Effect: "NoSchedule"},
			AllowedNamespaces: platformv1alpha1.NamespaceSelector{
				MatchNames: []string{"ml-platform"},
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
			},
		},
	}
}

func gpuPodJSON(namespace string) []byte {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: namespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "c",
				Image: "cuda",
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
	raw, _ := json.Marshal(pod)
	return raw
}

func plainPodJSON() []byte {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx"}}},
	}
	raw, _ := json.Marshal(pod)
	return raw
}

func TestPodValidator_AllowsNonGPU(t *testing.T) {
	scheme := testScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(testPolicy(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}).
		Build()
	v := &PodValidator{Client: c, Decoder: admission.NewDecoder(scheme)}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Object:    runtime.RawExtension{Raw: plainPodJSON()},
		},
	}
	resp := v.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Fatalf("expected allowed, got %v", resp.Result.Message)
	}
}

func TestPodValidator_DeniesUnauthorizedNamespace(t *testing.T) {
	scheme := testScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(testPolicy(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}).
		Build()
	v := &PodValidator{Client: c, Decoder: admission.NewDecoder(scheme)}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Object:    runtime.RawExtension{Raw: gpuPodJSON("default")},
		},
	}
	resp := v.Handle(context.Background(), req)
	if resp.Allowed {
		t.Fatal("expected denied for unauthorized namespace")
	}
}

func TestPodMutator_InjectsForAuthorizedNamespace(t *testing.T) {
	scheme := testScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(testPolicy(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}).
		Build()
	m := &PodMutator{Client: c, Decoder: admission.NewDecoder(scheme)}
	raw := gpuPodJSON("ml-platform")
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "ml-platform",
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
	resp := m.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Fatalf("expected allowed mutation, got %v", resp.Result)
	}
}

func TestPodValidator_AuditModeAllowsWithWarning(t *testing.T) {
	scheme := testScheme()
	pol := testPolicy()
	pol.Spec.EnforcementMode = platformv1alpha1.EnforcementAudit
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pol, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}).
		Build()
	v := &PodValidator{Client: c, Decoder: admission.NewDecoder(scheme)}
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "ml-platform",
			Object:    runtime.RawExtension{Raw: gpuPodJSON("ml-platform")},
		},
	}
	resp := v.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Fatal("audit mode should allow pod with violations")
	}
	if len(resp.Warnings) == 0 {
		t.Fatal("expected audit warnings")
	}
}
