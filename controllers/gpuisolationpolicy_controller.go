/*
Copyright 2026 Platform Engineering.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
	"github.com/operator/gpu-isolation-operator/internal/policy"
)


// GPUIsolationPolicyReconciler reconciles GPUIsolationPolicy cluster resources.
type GPUIsolationPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=platform.example.com,resources=gpuisolationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.example.com,resources=gpuisolationpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.example.com,resources=gpuisolationpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=get;list;watch;create;update;patch

func (r *GPUIsolationPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pol platformv1alpha1.GPUIsolationPolicy
	if err := r.Get(ctx, req.NamespacedName, &pol); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	active, err := policy.ResolveActivePolicy(ctx, r.Client)
	if err != nil {
		return r.setNotReady(ctx, &pol, err.Error())
	}
	if active.Name != pol.Name {
		msg := fmt.Sprintf("policy %q is not active; active policy is %q", pol.Name, active.Name)
		return r.setNotReady(ctx, &pol, msg)
	}

	if err := r.reconcilePriorityClass(ctx, &pol); err != nil {
		return r.setNotReady(ctx, &pol, fmt.Sprintf("priorityClass: %v", err))
	}

	managedNodes, err := r.reconcileNodeTaints(ctx, &pol)
	if err != nil {
		return r.setNotReady(ctx, &pol, fmt.Sprintf("node taints: %v", err))
	}

	managedNamespaces, err := r.reconcileResourceQuotas(ctx, &pol)
	if err != nil {
		return r.setNotReady(ctx, &pol, fmt.Sprintf("resource quotas: %v", err))
	}

	now := metav1.Now()
	pol.Status.ObservedGeneration = pol.Generation
	pol.Status.Ready = true
	pol.Status.Message = "reconciled successfully"
	pol.Status.ManagedPriorityClass = pol.Spec.PriorityClass.Name
	pol.Status.ManagedNamespaces = managedNamespaces
	pol.Status.ManagedGPUNodes = managedNodes
	pol.Status.LastReconcileTime = &now
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.Status().Update(ctx, &pol)
}

func (r *GPUIsolationPolicyReconciler) setNotReady(ctx context.Context, pol *platformv1alpha1.GPUIsolationPolicy, msg string) (ctrl.Result, error) {
	pol.Status.ObservedGeneration = pol.Generation
	pol.Status.Ready = false
	pol.Status.Message = msg
	now := metav1.Now()
	pol.Status.LastReconcileTime = &now
	_ = r.Status().Update(ctx, pol)
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *GPUIsolationPolicyReconciler) reconcilePriorityClass(ctx context.Context, pol *platformv1alpha1.GPUIsolationPolicy) error {
	desired := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: pol.Spec.PriorityClass.Name,
			Labels: map[string]string{
				platformv1alpha1.LabelManagedBy: "gpu-isolation-operator",
				"platform.example.com/policy":   pol.Name,
			},
		},
		Value:            pol.Spec.PriorityClass.Value,
		GlobalDefault:    pol.Spec.PriorityClass.GlobalDefault,
		Description:      pol.Spec.PriorityClass.Description,
		PreemptionPolicy: schedulingPreemptionPolicy(),
	}

	existing := &schedulingv1.PriorityClass{}
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Value = desired.Value
	existing.GlobalDefault = desired.GlobalDefault
	existing.Description = desired.Description
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
}

func schedulingPreemptionPolicy() *corev1.PreemptionPolicy {
	p := corev1.PreemptLowerPriority
	return &p
}

func (r *GPUIsolationPolicyReconciler) reconcileNodeTaints(ctx context.Context, pol *platformv1alpha1.GPUIsolationPolicy) ([]string, error) {
	var nodeList corev1.NodeList
	if err := r.List(ctx, &nodeList); err != nil {
		return nil, err
	}

	taint := policy.BuildGPUTaint(pol)
	var managed []string
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !policy.NodeMatchesGPUNodeSelector(node, pol.Spec.GPUNodeSelector) {
			continue
		}
		if policy.NodeHasGPUTaint(node, pol) {
			managed = append(managed, node.Name)
			continue
		}

		patch := client.MergeFrom(node.DeepCopy())
		node.Spec.Taints = append(node.Spec.Taints, taint)
		if err := r.Patch(ctx, node, patch); err != nil {
			return managed, fmt.Errorf("patch node %s: %w", node.Name, err)
		}
		managed = append(managed, node.Name)
	}
	return managed, nil
}

func (r *GPUIsolationPolicyReconciler) reconcileResourceQuotas(ctx context.Context, pol *platformv1alpha1.GPUIsolationPolicy) ([]string, error) {
	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList); err != nil {
		return nil, err
	}

	var managed []string
	for i := range nsList.Items {
		ns := &nsList.Items[i]
		if ns.Name == "kube-system" || ns.Name == "kube-public" || ns.Name == "kube-node-lease" {
			continue
		}

		allowed := policy.NamespaceAllowed(ns, pol)
		var desired *corev1.ResourceQuota
		if allowed {
			desired = policy.BuildAllowedResourceQuota(pol.Name, ns.Name, pol)
		} else if pol.Spec.Quota.DefaultDenyGpu {
			desired = policy.BuildDenyResourceQuota(pol.Name, ns.Name, pol)
		} else {
			continue
		}

		existing := &corev1.ResourceQuota{}
		key := client.ObjectKey{Name: desired.Name, Namespace: ns.Name}
		err := r.Get(ctx, key, existing)
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, desired); err != nil {
				return managed, err
			}
			managed = append(managed, ns.Name)
			continue
		}
		if err != nil {
			return managed, err
		}

		if !reflect.DeepEqual(existing.Spec.Hard, desired.Spec.Hard) || !mapsEqual(existing.Labels, desired.Labels) {
			patch := client.MergeFrom(existing.DeepCopy())
			existing.Spec = desired.Spec
			existing.Labels = desired.Labels
			if err := r.Patch(ctx, existing, patch); err != nil {
				return managed, err
			}
		}
		managed = append(managed, ns.Name)
	}
	return managed, nil
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// SetupWithManager registers the reconciler with the Manager.
func (r *GPUIsolationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.GPUIsolationPolicy{}).
		Owns(&corev1.ResourceQuota{}).
		Named("gpuisolationpolicy").
		Complete(r)
}
