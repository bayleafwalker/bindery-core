{{- define "bindery-external-runtime.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "bindery-external-runtime.fullname" -}}
{{- if .Values.fullnameOverride }}{{ .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{ else }}{{ include "bindery-external-runtime.name" . }}{{ end }}
{{- end }}

{{- define "bindery-external-runtime.labels" -}}
app.kubernetes.io/name: {{ include "bindery-external-runtime.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "bindery-external-runtime.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}{{ default (include "bindery-external-runtime.fullname" .) .Values.serviceAccount.name }}{{ else }}{{ default "default" .Values.serviceAccount.name }}{{ end }}
{{- end }}

