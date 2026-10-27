# Configuration Guide

Complete reference for uMailServer configuration.

## Configuration File

Default location: `/etc/umailserver/umailserver.yaml`

## Server Settings

```yaml
server:
  hostname: mail.example.com      # Primary server hostname (must match TLS cert)
  data_dir: /var/lib/umailserver  # Where all data is stored
  force_close_after: 30           # Seconds before shutdown force-closes SMTP sessions and HTTP connections
  graceful_timeout: 30            # Seconds shutdown waits for in-flight background tasks (default 10)
```

The log level lives in the [`logging`](#logging) section.

## TLS Configuration

### Let's Encrypt (Recommended)

```yaml
tls:
  acme:
    enabled: true
    email: admin@example.com
    provider: letsencrypt          # letsencrypt or letsencrypt-staging

  # Minimum TLS version for every TLS listener: "1.2" or "1.3"
  min_version: "1.2"
```

Certificates are issued with the TLS-ALPN-01 challenge on the HTTPS port: there
is no port-80 listener and no DNS-01 support, so use `cert_file`/`key_file`
when the HTTPS port is not reachable from the internet. Cipher suites are fixed
to ECDHE with AES-GCM or ChaCha20-Poly1305.

### Custom Certificates

```yaml
tls:
  acme:
    enabled: false

  cert_file: /etc/umailserver/certs/mail.crt
  key_file: /etc/umailserver/certs/mail.key
```

## SMTP Configuration

### Inbound SMTP (Port 25)

```yaml
smtp:
  max_connections_per_ip: 50     # Per-client-IP cap on all SMTP listeners (0 = default 50, negative = disabled)
  inbound:
    enabled: true
    port: 25
    bind: 0.0.0.0
    max_message_size: 52428800     # 50MB in bytes
    max_recipients: 100            # Max RCPT TO per message
    max_connections: 100           # Max concurrent connections on this listener
    read_timeout: 5m
    write_timeout: 5m
```

### Submission (Port 587)

```yaml
smtp:
  submission:
    enabled: true
    port: 587
    require_tls: true              # Require STARTTLS before AUTH
```

### Implicit TLS (Port 465)

```yaml
smtp:
  submission_tls:
    enabled: true
    port: 465
```

Submission listeners always require authentication; `require_auth: false` is
ignored with a warning.

## IMAP Configuration

```yaml
imap:
  enabled: true
  port: 993                        # Implicit TLS
  bind: 0.0.0.0
  starttls_port: 143               # Plain IMAP upgraded with STARTTLS (0 = disabled)
  max_connections: 10000           # Max concurrent connections (0 = unlimited)
  idle_timeout: 30m
```

## HTTP/Web Configuration

```yaml
http:
  enabled: true
  port: 443
  bind: 0.0.0.0

  # CORS settings (for development)
  cors_origins: []                 # ["http://localhost:5173"]

  # Reverse proxies allowed to set X-Forwarded-For (IPs or CIDRs). Rate
  # limits and the audit log use the forwarded client address only for
  # requests coming from these addresses.
  trusted_proxies: []
```

Webmail and the admin UI are embedded in the binary. The API always starts;
`enabled: false` is ignored with a warning.

## Admin Panel

The admin API is also served on a separate plain-HTTP listener. Keep it on
loopback and reach it through an SSH tunnel or a reverse proxy.

```yaml
admin:
  enabled: true
  bind: 127.0.0.1
  port: 8443
```

## Security

```yaml
security:
  jwt_secret: "change-me-to-a-32-character-secret-minimum"
  max_login_attempts: 5            # Failed logins before lockout
  lockout_duration: 15m
  # totp_key: ""                   # >= 32 chars; encrypts TOTP secrets (defaults to jwt_secret)

  audit_log:
    path: /var/lib/umailserver/logs/audit.log   # Empty = audit log disabled
    max_size_mb: 10
    max_backups: 5
    max_age_days: 30

  rate_limit:
    ip_per_minute: 30              # Inbound messages per client IP
    ip_per_hour: 500
    ip_connections: 10             # Concurrent connections per IP
    user_per_minute: 60            # Authenticated outbound messages per user
    user_per_hour: 1000
    user_max_recipients: 100       # Recipients per message
    http_requests_per_minute: 120  # API requests per minute per IP
```

See `umailserver.yaml.example` for the full list of rate limits. Two-factor
authentication (TOTP) is enabled per account in webmail and needs no
configuration.

## Spam Filtering

```yaml
spam:
  enabled: true

  # Score thresholds
  reject_threshold: 9.0            # Reject at SMTP level
  junk_threshold: 3.0              # Deliver to Junk folder

  # RBL/DNSBL servers
  rbl_servers:
    - zen.spamhaus.org
    - b.barracudacentral.org
    - bl.spamcop.net

  # Greylisting
  greylisting:
    enabled: true
    delay: 5m                      # Minimum wait before a retried sender is accepted (max 6h)

  # Bayesian classifier
  bayesian:
    enabled: true
    # Train from user actions: a message moved or copied into Junk is
    # learned as spam, one moved out of Junk (except into Trash) as ham.
    # The classifier scores nothing until it has learned 10 messages of each.
    auto_train: true
```

## DKIM Signing

DKIM is not configured in the YAML file. `umailserver domain add example.com`
generates a key pair for the domain and stores it in the database; outbound
mail from the domain is signed with it (relaxed header, simple body canonicalization).
`umailserver domain dns example.com` prints the DNS records to publish,
including `default._domainkey.example.com`.

## Domain Configuration

Domains are not configured in the YAML file. A top-level `domains:` list is accepted for
backward compatibility but ignored (a warning is logged at load); create domains with the
CLI or the admin API.

## Storage

Messages are stored as Maildir++ under `<data_dir>/mail` and every message
file is fsynced before delivery is acknowledged. The `storage` section has no
working settings: `sync: false` is ignored with a warning, and
`shared_folders` is accepted but has no effect. Mailbox quotas are set per
account through the admin API.

## Queue Settings

```yaml
queue:
  max_age: 120h                     # Maximum time in queue before bounce (default 120h; allowed 105h-720h, unset = default)
```

Deferred messages are retried on a built-in schedule that spans 105 hours,
which is why `max_age` cannot be shorter.

## DMARC Reports

```yaml
dmarc:
  enabled: true
  org_name: Example Mail          # Reporting organization named in reports
  from_email: dmarc-reports@example.com  # Sender of report messages (required)
  report_email: ""                # Contact address in reports (default: from_email)
  interval: 24h                   # How often reports are sent (minimum 1h)
```

Inbound mail is evaluated against the sender's DMARC policy either way.
With reporting enabled, the results for domains whose record has a
`rua=mailto:` address are collected and, every `interval`, sent there as
RFC 7489 aggregate reports (gzip-compressed XML). A report for a domain goes
to an address in another domain only if that domain publishes the
`<domain>._report._dmarc.<dest>` authorization record. Results are kept in
memory: those not yet reported when the server restarts are lost, and a
report that cannot be queued is dropped.

## Monitoring

```yaml
# Prometheus metrics: a separate listener serving metrics.path and /healthz
metrics:
  enabled: true
  bind: "127.0.0.1"
  port: 8080
  path: /metrics

# Health checks (GET /health, GET /health/ready) are served by the web listener
# (http.port) and need no configuration.

# Distributed tracing (OpenTelemetry), a top-level section
tracing:
  enabled: false
  service_name: "umailserver"
  exporter: "noop"                # Options: noop, stdout, otlp
  otlp_endpoint: "localhost:4317" # OTLP collector endpoint
  insecure: false                 # allow plaintext OTLP to a non-loopback collector (default: refused; use https://)
  environment: "production"       # Environment tag
  sample_rate: 1.0                # 0.0 to 1.0 (1.0 = trace all)
  attributes:                     # Additional resource attributes
    deployment: "primary"
    region: "us-east-1"
```

## Logging

```yaml
logging:
  level: info                     # debug, info, warn, error
  format: json                    # json, text
  output: /var/log/umailserver.log  # stdout, stderr, or a file path
  max_size_mb: 100                # Rotation size (file output only)
  max_backups: 5
  max_age_days: 30
```

## Environment Variables

All config options can be overridden via environment variables:

```bash
# Format: UMAILSERVER_<SECTION>_<KEY>
export UMAILSERVER_SERVER_HOSTNAME=mail.example.com
export UMAILSERVER_SERVER_DATA_DIR=/data
export UMAILSERVER_TLS_ACME_ENABLED=true
export UMAILSERVER_SMTP_INBOUND_PORT=25
export UMAILSERVER_IMAP_PORT=993
export UMAILSERVER_SPAM_REJECT_THRESHOLD=9.0
```

## Complete Example

See [config.example.yaml](../config/config.example.yaml) for a full production-ready configuration.
