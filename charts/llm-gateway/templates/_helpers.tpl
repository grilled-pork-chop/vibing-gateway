{{/*
Inlined helpers for the standalone llm-gateway chart (no shared library).
Kept narrow: just the label/annotation set the templates emit, plus the HTTPS
listener guard folded in from the former _listeners.tpl.
*/}}

{{/* Chart name-version label. */}}
{{- define "llm-gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard labels — applied to every resource.
Usage:  {{- include "llm-gateway.labels" . | nindent 4 }}
*/}}
{{- define "llm-gateway.labels" -}}
helm.sh/chart: {{ include "llm-gateway.chart" . }}
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
Usage:  {{- include "llm-gateway.annotations" . | nindent 2 }}
*/}}
{{- define "llm-gateway.annotations" -}}
{{- with .Values.commonAnnotations }}
annotations:
{{ toYaml . | indent 2 -}}
{{- end }}
{{- end -}}

{{/*
models-aggregator resource name (Deployment / Service / ServiceAccount / RBAC / route).
*/}}
{{- define "llm-gateway.modelsAggregator.name" -}}
{{- printf "%s-models-aggregator" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Pod selector labels for the models-aggregator — kept constant so the Service matches the pods.
*/}}
{{- define "llm-gateway.modelsAggregator.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: models-aggregator
{{- end -}}

{{/*
Fully-qualified models-aggregator image ref (must match the images.txt entry).
*/}}
{{- define "llm-gateway.modelsAggregator.image" -}}
{{- $img := .Values.modelsEndpoint.image -}}
{{- printf "%s/%s:%s" $img.registry $img.repository $img.tag -}}
{{- end -}}

{{/*
Returns "true" if any value in .Values.listeners already declares HTTPS, so the
Gateway template doesn't double-emit a 443 listener when TLS is enabled.
*/}}
{{- define "gateway.hasHttpsListener" -}}
{{- $found := false -}}
{{- range .Values.listeners -}}
  {{- if eq .protocol "HTTPS" -}}{{- $found = true -}}{{- end -}}
{{- end -}}
{{- if $found }}true{{- end -}}
{{- end -}}
