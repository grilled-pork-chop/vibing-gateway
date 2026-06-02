{{/*
Inlined helpers for the standalone slurm-models chart (no shared library).
Kept narrow: just the label/annotation set the templates emit. Mirrors the
llm-gateway chart's helpers.
*/}}

{{/* Chart name-version label. */}}
{{- define "slurm-models.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard labels — applied to every resource.
Usage:  {{- include "slurm-models.labels" . | nindent 4 }}
*/}}
{{- define "slurm-models.labels" -}}
helm.sh/chart: {{ include "slurm-models.chart" . }}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: llm-gateway
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/*
Common annotations — emitted as an `annotations:` block only when non-empty.
Usage:  {{- include "slurm-models.annotations" . | nindent 2 }}
*/}}
{{- define "slurm-models.annotations" -}}
{{- with .Values.commonAnnotations }}
annotations:
{{ toYaml . | indent 2 -}}
{{- end }}
{{- end -}}
