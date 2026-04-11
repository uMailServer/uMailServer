# Configuration Guide

Complete reference for uMailServer configuration.

## Configuration File

Default location: `/etc/umailserver/umailserver.yaml`

## Server Settings

```yaml
server:
  hostname: mail.example.com      # Primary server hostname (must match TLS cert)
  data_dir: /var/lib/umailserver  # Where all data is stored
  max_workers: 100                # Maximum concurrent workers
  log_level: info                 # debug, info, warn, error
```

## TLS Configuration

### Let's Encrypt (Recommended)

```yaml
tls:
  acme:
    enabled: true
    email: admin@example.com
    provider: letsencrypt          # letsencrypt, zerossl, buypass
    accept_tos: true
    http_challenge: true           # HTTP-01 challenge
    tls_alpn: false                # TLS-ALPN-01 challenge

  # Minimum TLS version
  min_version: "1.2"

  # Cipher suites (defaults are secure)
  cipher_suites:
    - TLS_AES_256_GCM_SHA384
    - TLS_CHACHA20_POLY1305_SHA256
    - TLS_AES_128_GCM_SHA256
```

### Custom Certificates

```yaml
tls:
  acme:
    enabled: false

  certificates:
    - domain: mail.example.com
      cert_file: /etc/umailserver/certs/mail.crt
      key_file: /etc/umailserver/certs/mail.key
```

## SMTP Configuration

### Inbound SMTP (Port 25)

```yaml
smtp:
  inbound:
    enabled: true
    port: 25
    bind: 0.0.0.0
    max_message_size: 52428800     # 50MB in bytes
    max_recipients: 100            # Max RCPT TO per message
    max_connections: 100           # Per-IP connection limit
    read_timeout: 5m
    write_timeout: 5m
```

### Submission (Port 587)

```yaml
smtp:
  submission:
    enabled: true
    port: 587
    require_auth: true             # Require authentication
    require_tls: true              # Require STARTTLS before AUTH
```

### Implicit TLS (Port 465)

```yaml
smtp:
  submission_tls:
    enabled: true
    port: 465
    require_auth: true
```

## IMAP Configuration

```yaml
imap:
  enabled: true
  port: 993
  bind: 0.0.0.0

  # Enable plain IMAP on port 143 (not recommended)
  insecure_port: 0

  # Connection limits
  max_connections: 1000
  max_connections_per_user: 10

  # Timeouts
  idle_timeout: 30m
  auth_timeout: 5m
```

## HTTP/Web Configuration

```yaml
http:
  enabled: true
  port: 443
  http_port: 80                    # Redirects to HTTPS
  bind: 0.0.0.0

  # Static files (webmail, admin panel)
  static_path: /usr/share/umailserver/web

  # CORS settings (for development)
  cors_origins: []                 # ["http://localhost:5173"]
```

## Admin Panel

The admin panel is served by the HTTP server on the same port (443).
The `admin` config section below exists for legacy/compatibility purposes
but is not currently used — admin panel access is controlled by the HTTP server.

```yaml
admin:
  enabled: true
  # port: 8443  # Note: Not currently used, admin served on HTTP port
  bind: 127.0.0.1                  # Note: Not currently used
```

## Authentication

```yaml
auth:
  # Password requirements
  min_password_length: 8
  password_complexity: medium      # low, medium, high

  # Brute force protection
  max_login_attempts: 5
  lockout_duration: 30m

  # Two-factor authentication
  totp:
    enabled: true
    issuer: uMailServer

  # App passwords for clients that don't support 2FA
  app_passwords:
    enabled: true
    max_per_user: 10
```

## Spam Filtering

```yaml
spam:
  enabled: true

  # Score thresholds
  reject_threshold: 9.0            # Reject at SMTP level
  junk_threshold: 6.0              # Deliver to Junk folder
  spam_threshold: 3.0              # Add X-Spam-Score header

  # RBL/DNSBL servers
  rbl_servers:
    - zen.spamhaus.org
    - b.barracudacentral.org
    - bl.spamcop.net

  # Greylisting
  greylisting:
    enabled: true
    delay: 5m                      # Initial delay
    max_record_age: 24h            # How long to remember good senders

  # Bayesian classifier
  bayesian:
    enabled: true
    auto_learn: true               # Learn from user actions
    min_tokens: 10                 # Minimum tokens to classify
```

## DKIM Signing

```yaml
dkim:
  enabled: true
  selector: default                # DNS record: <selector>._domainkey.<domain>
  domain: example.com
  key_file: /var/lib/umailserver/dkim/example.com.private.pem

  # Signing options
  sign_headers:
    - From
    - To
    - Subject
    - Date
    - Message-ID

  # Canonicalization
  header_canonicalization: relaxed
  body_canonicalization: relaxed
```

## Domain Configuration

```yaml
domains:
  - name: example.com
    max_accounts: 100
    max_mailbox_size: 5368709120   # 5GB in bytes
    max_message_size: 52428800     # 50MB
    is_active: true

    # DKIM for this domain
    dkim:
      selector: default
      key_file: /var/lib/umailserver/dkim/example.com.private.pem

    # Aliases
    aliases:
      - alias: "postmaster"
        target: "admin"
      - alias: "abuse"
        target: "admin"
      - alias: "@"                   # Catch-all
        target: "admin"

  - name: another-domain.com
    max_accounts: 50
    # ...
```

## Storage

```yaml
storage:
  type: maildir                     # maildir, s3 (future)
  path: /var/lib/umailserver/mail

  # Quota settings
  quota_grace_period: 7d            # Allow over-quota for 7 days
  quota_warning_threshold: 0.9      # Warn at 90% full

  # Message retention
  retention:
    junk: 30d                       # Auto-delete junk after 30 days
    trash: 30d                      # Auto-delete trash after 30 days
```

## Queue Settings

```yaml
queue:
  retry_schedule:                   # Retry delays
    - 5m
    - 15m
    - 1h
    - 2h
    - 4h
    - 8h
    - 12h

  max_retries: 7                    # After this, generate bounce
  max_age: 48h                      # Maximum time in queue

  # Concurrency
  max_concurrent_deliveries: 50
  max_concurrent_per_domain: 5      # Rate limit per destination
```

## Monitoring

```yaml
monitoring:
  # Prometheus metrics
  prometheus:
    enabled: true
    port: 9090
    path: /metrics

  # Health check endpoint
  health:
    enabled: true
    port: 8080
    path: /health

  # Structured logging
  logging:
    format: json                    # json, text
    output: /var/log/umailserver.log
    max_size: 100MB
    max_backups: 5
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
