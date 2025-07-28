{{/*
Expand the name of the chart.
*/}}
{{- define "notificator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "notificator.fullname" -}}
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
{{- define "notificator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "notificator.labels" -}}
helm.sh/chart: {{ include "notificator.chart" . }}
{{ include "notificator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "notificator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "notificator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "notificator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "notificator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Backwards compatibility - keep old function names
*/}}
{{- define "notificator-backend.name" -}}
{{- include "notificator.name" . }}
{{- end }}

{{- define "notificator-backend.fullname" -}}
{{- include "notificator.fullname" . }}
{{- end }}

{{- define "notificator-backend.chart" -}}
{{- include "notificator.chart" . }}
{{- end }}

{{- define "notificator-backend.labels" -}}
{{- include "notificator.labels" . }}
{{- end }}

{{- define "notificator-backend.selectorLabels" -}}
{{- include "notificator.selectorLabels" . }}
{{- end }}

{{- define "notificator-backend.serviceAccountName" -}}
{{- include "notificator.serviceAccountName" . }}
{{- end }}