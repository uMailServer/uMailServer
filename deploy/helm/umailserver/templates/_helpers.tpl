{{- define "umailserver.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "umailserver.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else if contains (include "umailserver.name" .) .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "umailserver.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "umailserver.selectorLabels" -}}
app.kubernetes.io/name: {{ include "umailserver.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "umailserver.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "umailserver.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "umailserver.jwtSecretName" -}}
{{- .Values.existingSecret | default (include "umailserver.fullname" .) }}
{{- end }}

{{- define "umailserver.jwtSecretKey" -}}
{{- if .Values.existingSecret }}{{ .Values.existingSecretKey }}{{ else }}jwt-secret{{ end }}
{{- end }}

{{/*
The settings the chart owns: they must match the container ports, the
volume and the TLS mount, so they override the same keys in .Values.config.
*/}}
{{- define "umailserver.chartConfig" -}}
{{- $p := .Values.ports -}}
server:
  hostname: {{ required "hostname is required" .Values.hostname | quote }}
  data_dir: /var/lib/umailserver
database:
  path: /var/lib/umailserver/umailserver.db
smtp:
  inbound:
    enabled: {{ $p.smtp.enabled }}
    bind: 0.0.0.0
    port: {{ $p.smtp.targetPort }}
  submission:
    enabled: {{ $p.submission.enabled }}
    bind: 0.0.0.0
    port: {{ $p.submission.targetPort }}
  submission_tls:
    enabled: {{ $p.submissions.enabled }}
    bind: 0.0.0.0
    port: {{ $p.submissions.targetPort }}
imap:
  enabled: {{ or $p.imap.enabled $p.imaps.enabled }}
  bind: 0.0.0.0
  port: {{ $p.imaps.targetPort }}
  starttls_port: {{ $p.imap.targetPort }}
pop3:
  enabled: {{ $p.pop3s.enabled }}
  bind: 0.0.0.0
  port: {{ $p.pop3s.targetPort }}
managesieve:
  enabled: {{ $p.managesieve.enabled }}
  bind: 0.0.0.0
  port: {{ $p.managesieve.targetPort }}
http:
  bind: 0.0.0.0
  port: {{ $p.https.targetPort }}
metrics:
  enabled: true
  bind: 0.0.0.0
  port: {{ .Values.metricsPort }}
{{- if .Values.tls.secretName }}
tls:
  cert_file: /etc/umailserver/tls/tls.crt
  key_file: /etc/umailserver/tls/tls.key
{{- end }}
{{- end }}

{{/* umailserver.yaml: .Values.config with the chart-owned keys on top. */}}
{{- define "umailserver.config" -}}
{{- $user := deepCopy (.Values.config | default dict) -}}
{{- $chart := include "umailserver.chartConfig" . | fromYaml -}}
{{- $cfg := mergeOverwrite $user $chart -}}
{{- /* mergeOverwrite skips false values, so a listener the chart turns off
could stay enabled by .Values.config; set the switches explicitly. */ -}}
{{- $p := .Values.ports -}}
{{- $_ := set $cfg.smtp.inbound "enabled" $p.smtp.enabled -}}
{{- $_ := set $cfg.smtp.submission "enabled" $p.submission.enabled -}}
{{- $_ := set $cfg.smtp.submission_tls "enabled" $p.submissions.enabled -}}
{{- $_ := set $cfg.imap "enabled" (or $p.imap.enabled $p.imaps.enabled) -}}
{{- $_ := set $cfg.pop3 "enabled" $p.pop3s.enabled -}}
{{- $_ := set $cfg.managesieve "enabled" $p.managesieve.enabled -}}
{{- toYaml $cfg }}
{{- end }}
