package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
)

var _ = Describe("GPUIsolationPolicy controller", func() {
	const policyName = "test-gpu-policy"

	ctx := context.Background()

	BeforeEach(func() {
		policy := &platformv1alpha1.GPUIsolationPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: policyName,
				Labels: map[string]string{
					platformv1alpha1.LabelActivePolicy: "true",
				},
			},
			Spec: platformv1alpha1.GPUIsolationPolicySpec{
				EnforcementMode:  platformv1alpha1.EnforcementEnforce,
				GPUResourceNames: []string{"nvidia.com/gpu"},
				GPUNodeSelector:  map[string]string{"accelerator": "nvidia-gpu"},
				GPUTaint: platformv1alpha1.TaintSpec{
					Key: "dedicated", Value: "gpu", Effect: string(corev1.TaintEffectNoSchedule),
				},
				AllowedNamespaces: platformv1alpha1.NamespaceSelector{
					MatchNames: []string{"ml-platform"},
				},
				PriorityClass: platformv1alpha1.PriorityClassSpec{
					Name: "gpu-high-priority", Value: 100000, Description: "GPU priority",
				},
				Quota: platformv1alpha1.QuotaSpec{
					DefaultDenyGpu:           true,
					AllowedNamespaceGpuLimit: "4",
				},
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
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())

		nsAllowed := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}}
		Expect(k8sClient.Create(ctx, nsAllowed)).To(Succeed())
		nsDenied := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
		Expect(k8sClient.Create(ctx, nsDenied)).To(Succeed())

		gpuNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "gpu-node-1",
				Labels: map[string]string{"accelerator": "nvidia-gpu"},
			},
		}
		Expect(k8sClient.Create(ctx, gpuNode)).To(Succeed())
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, &platformv1alpha1.GPUIsolationPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName}})
		_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ml-platform"}})
		_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
		_ = k8sClient.Delete(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-node-1"}})
	})

	It("reconciles PriorityClass, taints, and ResourceQuotas", func() {
		controllerReconciler := &GPUIsolationPolicyReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: policyName}})
		Expect(err).NotTo(HaveOccurred())

	Eventually(func(g Gomega) {
		var pol platformv1alpha1.GPUIsolationPolicy
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policyName}, &pol)).To(Succeed())
		g.Expect(pol.Status.Ready).To(BeTrue())
		g.Expect(pol.Status.ManagedPriorityClass).To(Equal("gpu-high-priority"))
	}, time.Second*10, time.Millisecond*250).Should(Succeed())

		var pc schedulingv1.PriorityClass
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gpu-high-priority"}, &pc)).To(Succeed())
		Expect(pc.Value).To(Equal(int32(100000)))

		var node corev1.Node
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gpu-node-1"}, &node)).To(Succeed())
		Expect(len(node.Spec.Taints)).To(BeNumerically(">=", 1))

		var allowedQuota corev1.ResourceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gpu-isolation-test-gpu-policy-ml-platform", Namespace: "ml-platform"}, &allowedQuota)).To(Succeed())
		allowedQty := allowedQuota.Spec.Hard[corev1.ResourceName("requests.nvidia.com/gpu")]
		Expect(allowedQty.Cmp(resource.MustParse("4"))).To(BeZero())

		var denyQuota corev1.ResourceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gpu-isolation-test-gpu-policy-default", Namespace: "default"}, &denyQuota)).To(Succeed())
		denyQty := denyQuota.Spec.Hard[corev1.ResourceName("requests.nvidia.com/gpu")]
		Expect(denyQty.Cmp(resource.MustParse("0"))).To(BeZero())
	})
})
