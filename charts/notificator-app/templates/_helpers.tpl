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

{{/*
Get all alertmanager configs including internal one if enabled
*/}}
{{- define "notificator.allAlertmanagers" -}}
{{- $alertmanagers := list }}
{{- if .Values.alertmanager.enabled }}
{{- $internal := dict "name" "Internal Alertmanager" "url" "http://notificator-alertmanager:9093" "username" "" "password" "" "token" "" "oauthEnabled" false "oauthProxyMode" true }}
{{- $alertmanagers = append $alertmanagers $internal }}
{{- end }}
{{- range .Values.alertmanagerConfig }}
{{- $alertmanagers = append $alertmanagers . }}
{{- end }}
{{- $alertmanagers | toJson }}
{{- end }}