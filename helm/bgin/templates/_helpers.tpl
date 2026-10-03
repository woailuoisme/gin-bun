{{/*
Expand the name of the chart.
*/}}
{{- define "bgin.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "bgin.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "bgin.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels (shared across all workloads)
*/}}
{{- define "bgin.labels" -}}
helm.sh/chart: {{ include "bgin.chart" . }}
{{ include "bgin.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Common base selector labels
*/}}
{{- define "bgin.selectorLabels" -}}
app.kubernetes.io/name: {{ include "bgin.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Selector labels for API
*/}}
{{- define "bgin.apiSelectorLabels" -}}
{{ include "bgin.selectorLabels" . }}
app.kubernetes.io/component: api
{{- end }}

{{/*
Selector labels for Worker
*/}}
{{- define "bgin.workerSelectorLabels" -}}
{{ include "bgin.selectorLabels" . }}
app.kubernetes.io/component: worker
{{- end }}

{{/*
Selector labels for Migration Job
*/}}
{{- define "bgin.migrationSelectorLabels" -}}
{{ include "bgin.selectorLabels" . }}
app.kubernetes.io/component: migration
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "bgin.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "bgin.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the secret name
*/}}
{{- define "bgin.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- printf "%s-secret" (include "bgin.fullname" .) }}
{{- end }}
{{- end }}
