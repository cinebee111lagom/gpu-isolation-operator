{{/*
Expand the name of the chart.
*/}}
{{- define "gpu-isolation-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "gpu-isolation-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version label.
*/}}
{{- define "gpu-isolation-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "gpu-isolation-operator.labels" -}}
helm.sh/chart: {{ include "gpu-isolation-operator.chart" . }}
{{ include "gpu-isolation-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: gpu-isolation-operator
{{- end }}

{{/*
Selector labels
*/}}
{{- define "gpu-isolation-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gpu-isolation-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Target namespace
*/}}
{{- define "gpu-isolation-operator.namespace" -}}
{{- .Values.namespace.name }}
{{- end }}

{{/*
Service account name
*/}}
{{- define "gpu-isolation-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "gpu-isolation-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Webhook Service name
*/}}
{{- define "gpu-isolation-operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "gpu-isolation-operator.fullname" .) }}
{{- end }}

{{/*
Webhook Service DNS names for cert-manager Certificate
*/}}
{{- define "gpu-isolation-operator.webhookServiceDNSNames" -}}
- {{ include "gpu-isolation-operator.webhookServiceName" . }}.{{ include "gpu-isolation-operator.namespace" . }}.svc
- {{ include "gpu-isolation-operator.webhookServiceName" . }}.{{ include "gpu-isolation-operator.namespace" . }}.svc.cluster.local
{{- end }}

{{/*
Mutating webhook configuration name
*/}}
{{- define "gpu-isolation-operator.mutatingWebhookName" -}}
{{- printf "%s-mutating" (include "gpu-isolation-operator.fullname" .) }}
{{- end }}

{{/*
Validating webhook configuration name
*/}}
{{- define "gpu-isolation-operator.validatingWebhookName" -}}
{{- printf "%s-validating" (include "gpu-isolation-operator.fullname" .) }}
{{- end }}

{{/*
ClusterRole name
*/}}
{{- define "gpu-isolation-operator.clusterRoleName" -}}
{{- printf "%s-manager" (include "gpu-isolation-operator.fullname" .) }}
{{- end }}

{{/*
Image reference
*/}}
{{- define "gpu-isolation-operator.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
cert-manager CA injection annotation value
*/}}
{{- define "gpu-isolation-operator.certManagerInjectCAFrom" -}}
{{- printf "%s/%s" (include "gpu-isolation-operator.namespace" .) .Values.certManager.certificate.name }}
{{- end }}

{{/*
Manager container args
*/}}
{{- define "gpu-isolation-operator.managerArgs" -}}
- --metrics-bind-address={{ .Values.manager.args.metricsBindAddress }}
- --health-probe-bind-address={{ .Values.manager.args.healthProbeBindAddress }}
- --webhook-cert-dir={{ .Values.manager.args.webhookCertDir }}
{{- if .Values.leaderElection.enabled }}
- --leader-elect
{{- end }}
{{- range .Values.manager.extraArgs }}
- {{ . }}
{{- end }}
{{- end }}
