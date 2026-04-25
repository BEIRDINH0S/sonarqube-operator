{{/*
Expand the name of the chart.
*/}}
{{- define "sonarqube-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a fully qualified app name. Truncated to 63 chars (DNS label limit).
If `fullnameOverride` is set we honor it verbatim. Otherwise we mimic the
chart-default helpers: <release>-<chart>, collapsed to <release> if the chart
name is already a prefix of the release.
*/}}
{{- define "sonarqube-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart label, e.g. sonarqube-operator-0.5.0 (chartversion is sanitized for labels).
*/}}
{{- define "sonarqube-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels applied to every resource. Includes the recommended Kubernetes
labels (app.kubernetes.io/*) plus the legacy `control-plane` label that the
metrics Service / ServiceMonitor selector still keys on.
*/}}
{{- define "sonarqube-operator.labels" -}}
helm.sh/chart: {{ include "sonarqube-operator.chart" . }}
{{ include "sonarqube-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: controller
app.kubernetes.io/part-of: sonarqube-operator
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels: the immutable subset that ends up in pod selectors. Never add
fields here without thinking — changing a selector on an existing Deployment
is a breaking change.
*/}}
{{- define "sonarqube-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "sonarqube-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end -}}

{{/*
Name of the ServiceAccount to use.
*/}}
{{- define "sonarqube-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "sonarqube-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Image reference: <repo>:<tag>. tag falls back to .Chart.AppVersion.
*/}}
{{- define "sonarqube-operator.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
Names of the webhook-related resources, kept in one place so they stay in sync
across the Service, ValidatingWebhookConfiguration, Certificate and the
cert-manager inject-ca-from annotation.
*/}}
{{- define "sonarqube-operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "sonarqube-operator.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sonarqube-operator.webhookSecretName" -}}
{{- if .Values.webhook.existingSecret -}}
{{- .Values.webhook.existingSecret -}}
{{- else -}}
{{- printf "%s-webhook-tls" (include "sonarqube-operator.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "sonarqube-operator.webhookCertificateName" -}}
{{- printf "%s-webhook" (include "sonarqube-operator.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "sonarqube-operator.webhookIssuerName" -}}
{{- printf "%s-selfsigned" (include "sonarqube-operator.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Whether the chart should provision its own self-signed cert-manager Issuer.
True only if cert-manager management is on AND no external issuerRef was given.
*/}}
{{- define "sonarqube-operator.webhook.useSelfSignedIssuer" -}}
{{- if and .Values.webhook.enabled .Values.webhook.certManager.enabled -}}
{{- if and (empty .Values.webhook.certManager.issuerRef.name) (empty .Values.webhook.certManager.issuerRef.kind) -}}
true
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Effective issuerRef for the Certificate. Defaults to the self-signed Issuer
the chart creates, otherwise honors what the user passed.
*/}}
{{- define "sonarqube-operator.webhook.issuerRef" -}}
{{- if include "sonarqube-operator.webhook.useSelfSignedIssuer" . -}}
name: {{ include "sonarqube-operator.webhookIssuerName" . }}
kind: Issuer
{{- else -}}
name: {{ required "webhook.certManager.issuerRef.name is required when issuerRef.kind is set" .Values.webhook.certManager.issuerRef.name }}
kind: {{ required "webhook.certManager.issuerRef.kind is required when issuerRef.name is set" .Values.webhook.certManager.issuerRef.kind }}
{{- end -}}
{{- end -}}

{{/*
inject-ca-from annotation value used on ValidatingWebhookConfiguration when
cert-manager is in charge of the webhook cert.
*/}}
{{- define "sonarqube-operator.webhook.injectCaFrom" -}}
{{- printf "%s/%s" .Release.Namespace (include "sonarqube-operator.webhookCertificateName" .) -}}
{{- end -}}
