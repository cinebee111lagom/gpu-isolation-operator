/*
Copyright 2026 Platform Engineering.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
	"github.com/operator/gpu-isolation-operator/internal/policy"
)

// PodMutator implements mutating admission for GPU Pods.
type PodMutator struct {
	Client  client.Client
	Decoder admission.Decoder
}

// Handle mutates authorized GPU Pods on CREATE.
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := m.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	pol, err := policy.ResolveActivePolicy(ctx, m.Client)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("resolve active policy: %w", err))
	}

	if !policy.PodRequestsGPU(pod, pol.Spec.GPUResourceNames) {
		return admission.Allowed("pod does not request GPU")
	}

	var ns corev1.Namespace
	if err := m.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, &ns); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("get namespace: %w", err))
	}

	if !policy.NamespaceAllowed(&ns, pol) {
		return admission.Allowed("namespace not authorized for GPU mutation")
	}

	if pol.Spec.EnforcementMode == platformv1alpha1.EnforcementAudit {
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations[platformv1alpha1.AnnotationPolicyName] = pol.Name
		violations := policy.ValidateGPUPod(pod, &ns, pol)
		if len(violations) > 0 {
			pod.Annotations[platformv1alpha1.AnnotationPolicyViolations] = policy.FormatViolations(violations)
		}
	} else {
		policy.MutateGPUPod(pod, pol)
	}

	marshaled, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}

// SetupPodMutatorWithManager registers the mutating webhook handler.
func SetupPodMutatorWithManager(mgr ctrl.Manager) error {
	mutator := &PodMutator{
		Client:  mgr.GetClient(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
	}
	mgr.GetWebhookServer().Register("/mutate-v1-pod-gpu-isolation", &admission.Webhook{Handler: mutator})
	return nil
}
