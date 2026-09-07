{{- define "pipelogiq.name" -}}{{ .Chart.Name }}{{- end -}}
{{- define "pipelogiq.tag" -}}{{ default .Chart.AppVersion .Values.image.tag }}{{- end -}}
{{- define "pipelogiq.labels" -}}
app.kubernetes.io/name: {{ include "pipelogiq.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ include "pipelogiq.tag" . | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
