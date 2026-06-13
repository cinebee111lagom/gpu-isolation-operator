// Package v1alpha1 contains API Schema definitions for the platform v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=platform.example.com
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// LabelActivePolicy marks the single active GPUIsolationPolicy instance.
	LabelActivePolicy = "platform.example.com/active"

	// AnnotationPolicyName records which policy mutated or audited a Pod.
	AnnotationPolicyName = "platform.example.com/gpu-policy"

	// AnnotationPolicyMutated indicates the Pod was mutated by the GPU policy webhook.
	AnnotationPolicyMutated = "platform.example.com/gpu-policy-mutated"

	// AnnotationPolicyViolations stores audit-mode violation messages on a Pod.
	AnnotationPolicyViolations = "platform.example.com/gpu-policy-violations"

	// LabelManagedBy identifies resources reconciled by this operator.
	LabelManagedBy = "platform.example.com/gpu-isolation-managed"

	// DefaultAllowedScheduler is the only scheduler allowed for GPU Pods unless extended.
	DefaultAllowedScheduler = "default-scheduler"
)

// EnforcementMode controls whether violations are denied or only audited.
// +kubebuilder:validation:Enum=Enforce;Audit
type EnforcementMode string

const (
	EnforcementEnforce EnforcementMode = "Enforce"
	EnforcementAudit   EnforcementMode = "Audit"
)

// GPUIsolationPolicySpec defines the desired GPU isolation policy.
type GPUIsolationPolicySpec struct {
	// EnforcementMode controls webhook enforcement behavior.
	// +kubebuilder:default=Enforce
	EnforcementMode EnforcementMode `json:"enforcementMode,omitempty"`

	// GPUResourceNames lists extended resource names treated as GPU devices.
	// +kubebuilder:validation:MinItems=1
	GPUResourceNames []string `json:"gpuResourceNames"`

	// GPUNodeSelector selects GPU nodes for taint management and affinity injection.
	GPUNodeSelector map[string]string `json:"gpuNodeSelector"`

	// GPUTaint is applied to nodes matching GPUNodeSelector.
	GPUTaint TaintSpec `json:"gpuTaint"`

	// AllowedNamespaces defines which namespaces may request GPU resources.
	AllowedNamespaces NamespaceSelector `json:"allowedNamespaces"`

	// PriorityClass defines the GPU-dedicated PriorityClass managed by the operator.
	PriorityClass PriorityClassSpec `json:"priorityClass"`

	// Quota controls namespace-level GPU ResourceQuota behavior.
	Quota QuotaSpec `json:"quota"`

	// Mutation controls automatic Pod field injection.
	Mutation MutationSpec `json:"mutation"`

	// Validation controls validating webhook checks.
	Validation ValidationSpec `json:"validation"`
}

// TaintSpec mirrors corev1.Taint fields used for GPU node isolation.
type TaintSpec struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// NamespaceSelector selects allowed namespaces by name and/or labels.
type NamespaceSelector struct {
	MatchNames  []string          `json:"matchNames,omitempty"`
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// PriorityClassSpec defines the managed PriorityClass.
type PriorityClassSpec struct {
	Name          string `json:"name"`
	Value         int32  `json:"value"`
	GlobalDefault bool   `json:"globalDefault,omitempty"`
	Description   string `json:"description,omitempty"`
}

// QuotaSpec defines ResourceQuota reconciliation behavior.
type QuotaSpec struct {
	DefaultDenyGpu             bool   `json:"defaultDenyGpu"`
	AllowedNamespaceGpuLimit   string `json:"allowedNamespaceGpuLimit,omitempty"`
	DenyUnauthorizedNamespaces bool   `json:"denyUnauthorizedNamespaces,omitempty"`
}

// MutationSpec toggles mutating webhook injections.
type MutationSpec struct {
	InjectToleration     bool `json:"injectToleration"`
	InjectNodeAffinity   bool `json:"injectNodeAffinity"`
	InjectPriorityClass  bool `json:"injectPriorityClass"`
}

// ValidationSpec toggles validating webhook checks.
type ValidationSpec struct {
	DenyGpuInUnauthorizedNamespace bool `json:"denyGpuInUnauthorizedNamespace"`
	DenyMissingPriorityClass       bool `json:"denyMissingPriorityClass"`
	DenyMissingToleration          bool `json:"denyMissingToleration"`
	DenyMissingNodeAffinity        bool `json:"denyMissingNodeAffinity"`
	DenyNodeNameBypass             bool `json:"denyNodeNameBypass"`
	DenyHostNetworkBypass          bool `json:"denyHostNetworkBypass,omitempty"`
	DenyUnauthorizedSchedulerName  bool `json:"denyUnauthorizedSchedulerName"`
	DenyIllegalGpuNodeAffinity     bool `json:"denyIllegalGpuNodeAffinity,omitempty"`
}

// GPUIsolationPolicyStatus defines the observed state of GPUIsolationPolicy.
type GPUIsolationPolicyStatus struct {
	ObservedGeneration   int64              `json:"observedGeneration,omitempty"`
	Ready                bool               `json:"ready,omitempty"`
	Message              string             `json:"message,omitempty"`
	ManagedPriorityClass string             `json:"managedPriorityClass,omitempty"`
	ManagedNamespaces    []string           `json:"managedNamespaces,omitempty"`
	ManagedGPUNodes      []string           `json:"managedGpuNodes,omitempty"`
	LastReconcileTime    *metav1.Time       `json:"lastReconcileTime,omitempty"`
	Conditions           []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=gpip
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.enforcementMode`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GPUIsolationPolicy is the Schema for the gpuisolationpolicies API.
type GPUIsolationPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUIsolationPolicySpec   `json:"spec,omitempty"`
	Status GPUIsolationPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GPUIsolationPolicyList contains a list of GPUIsolationPolicy.
type GPUIsolationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUIsolationPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GPUIsolationPolicy{}, &GPUIsolationPolicyList{})
}
