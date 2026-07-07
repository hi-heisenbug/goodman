{{- define "goodman.labels" -}}
app.kubernetes.io/name: goodman
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
goodman.io/cluster: {{ .Values.cluster | quote }}
{{- end -}}

{{- define "goodman.collectorDSN" -}}
{{- if .Values.postgres.dsn -}}
{{ .Values.postgres.dsn }}
{{- else -}}
{{ .Values.postgres.sqlitePath }}
{{- end -}}
{{- end -}}
