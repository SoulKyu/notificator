{{/*
Create the name of the service account to use for backend
*/}}
{{- define "notificator.backend.serviceAccountName" -}}
{{- if .Values.backend.serviceAccount.create }}
{{- default (printf "notificator-backend") .Values.backend.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.backend.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use for webui
*/}}
{{- define "notificator.webui.serviceAccountName" -}}
{{- if .Values.webui.serviceAccount.create }}
{{- default (printf "notificator-webui") .Values.webui.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.webui.serviceAccount.name }}
{{- end }}
{{- end }}

