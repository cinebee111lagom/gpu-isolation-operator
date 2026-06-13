/*
Copyright 2026 Platform Engineering.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package webhooks

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/operator/gpu-isolation-operator/internal/policy"
)

// PodValidator implements validating admission for GPU Pods.
type PodValidator struct {
	Client  client.Client
	Decoder admission.Decoder
}

// Handle validates GPU Pods on CREATE and UPDATE.
func (v *PodValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := v.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	pol, err := policy.ResolveActivePolicy(ctx, v.Client)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("resolve active policy: %w", err))
	}

	var ns corev1.Namespace
	if err := v.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, &ns); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("get namespace: %w", err))
	}

	requestsGPU := policy.PodRequestsGPU(pod, pol.Spec.GPUResourceNames)
	var violations []policy.Violation

	if requestsGPU {
		violations = policy.ValidateGPUPod(pod, &ns, pol)
	} else {
		violations = policy.ValidateNonGPUPod(pod, &ns, pol)
	}

	if len(violations) == 0 {
		return admission.Allowed("pod complies with GPU isolation policy")
	}

	msg := policy.FormatViolations(violations)
	if policy.IsEnforceMode(pol) {
		return admission.Denied(msg)
	}

	warnings := admission.Warnings{msg}
	return admission.Allowed("audit mode: violations recorded").WithWarnings(warnings...)
}

// SetupPodValidatorWithManager registers the validating webhook handler.
func SetupPodValidatorWithManager(mgr ctrl.Manager) error {
	validator := &PodValidator{
		Client:  mgr.GetClient(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
	}
	mgr.GetWebhookServer().Register("/validate-v1-pod-gpu-isolation", &admission.Webhook{Handler: validator})
	return nil
}
