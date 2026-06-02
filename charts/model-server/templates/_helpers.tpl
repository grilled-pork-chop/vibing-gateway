{{/*
Inlined helpers for the standalone model-server chart (no shared library).
The LLMInferenceService name is the helm release name (overridable via
fullnameOverride), so distinct model releases get distinct names / route paths.
*/}}

{{/* Resource name = release name (or fullnameOverride). */}}
{{- define "model-server.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Chart name-version label. */}}
{{- define "model-server.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard labels — applied to every resource.
Usage:  {{- include "model-server.labels" . | nindent 4 }}
*/}}
{{- define "model-server.labels" -}}
helm.sh/chart: {{ include "model-server.chart" . }}
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
Selector labels — stable subset; instance differentiates one model release from another.
*/}}
{{- define "model-server.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Common annotations — emitted as an `annotations:` block only when non-empty.
Usage:  {{- include "model-server.annotations" . | nindent 2 }}
*/}}
{{- define "model-server.annotations" -}}
{{- with .Values.commonAnnotations }}
annotations:
{{ toYaml . | indent 2 -}}
{{- end }}
{{- end -}}

{{/*
Render an image reference from a dict like:
  { image: <.Values.vllm.image>, context: . }
Honors .Values.imageRegistry as a registry override.
*/}}
{{- define "model-server.image" -}}
{{- $img := .image -}}
{{- $registry := coalesce .context.Values.imageRegistry $img.registry -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry $img.repository $img.tag -}}
{{- else -}}
{{- printf "%s:%s" $img.repository $img.tag -}}
{{- end -}}
{{- end -}}
