{{- define "goodman.labels" -}}
app.kubernetes.io/name: goodman
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
goodman.io/cluster: {{ .Values.cluster | quote }}
{{- end -}}

{{- define "goodman.authSecretName" -}}
{{- if .Values.auth.existingSecret -}}
{{ .Values.auth.existingSecret }}
{{- else -}}
{{ .Release.Name }}-auth
{{- end -}}
{{- end -}}

{{- define "goodman.collectorScheme" -}}
{{- if .Values.collector.tls.secretName -}}https{{- else -}}http{{- end -}}
{{- end -}}

{{- define "goodman.collectorDSN" -}}
{{- if .Values.postgres.dsn -}}
{{ .Values.postgres.dsn }}
{{- else -}}
{{ .Values.postgres.sqlitePath }}
{{- end -}}
{{- end -}}
