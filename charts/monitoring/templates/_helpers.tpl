{{/*
Inlined helpers for the standalone monitoring chart (no shared library), mirroring the
label/annotation set the other charts emit.
*/}}

{{/* Chart name-version label. */}}
{{- define "monitoring.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard labels — applied to every resource this chart owns.
Usage:  {{- include "monitoring.labels" . | nindent 4 }}
*/}}
{{- define "monitoring.labels" -}}
helm.sh/chart: {{ include "monitoring.chart" . }}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: llm-gateway
{{- end -}}
