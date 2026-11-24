{{- define "pipelogiq.name" -}}{{ .Chart.Name }}{{- end -}}
{{- define "pipelogiq.tag" -}}{{ default .Chart.AppVersion .Values.image.tag }}{{- end -}}
{{- define "pipelogiq.labels" -}}
app.kubernetes.io/name: {{ include "pipelogiq.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ include "pipelogiq.tag" . | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
{{- define "pipelogiq.basePath" -}}
{{- $path := default "" .Values.app.basePath -}}
{{- if and $path (not (regexMatch "^(/[a-zA-Z0-9_-]+)+$" $path)) -}}
{{- fail "app.basePath must be empty or a path such as /pipelogiq without a trailing slash" -}}
{{- end -}}
{{- $path -}}
{{- end -}}
