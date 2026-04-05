# uMailServer Security Hardening Guide

This guide provides security hardening recommendations for production deployments of uMailServer.

## Table of Contents

- [Secure Configuration](#secure-configuration)
- [Network Security](#network-security)
- [Authentication Security](#authentication-security)
- [TLS Configuration](#tls-configuration)
- [API Security](#api-security)
- [Operational Security](#operational-security)
- [Security Headers](#security-headers)
- [Input Validation](#input-validation)
- [Audit Logging](#audit-logging)

## Secure Configuration

### File Permissions

Ensure proper file permissions:

```bash
# Configuration files should be readable only by owner
chmod 600 /etc/umailserver/umailserver.yaml

# Data directory should be owned by umailserver user
chown -R umailserver:umailserver /var/lib/umailserver
chmod 750 /var/lib/umailserver

# Log files should be writable only by umailserver
chmod 750 /var/log/umailserver
```

### Environment Variables

Never pass secrets via command line. Use environment variables or secure secret management:

```bash
# Set in /etc/umailserver/umailserver.yaml or environment
export UMAILSERVER_SECURITY_JWTSECRET="$(openssl rand -base64 32)"
```

## Network Security

### Firewall Rules

```bash
# Allow required ports
ufw allow 25/tcp      # SMTP
ufw allow 465/tcp     # SMTPS
ufw allow 587/tcp     # Submission
ufw allow 993/tcp     # IMAPS
ufw allow 995/tcp     # POP3S

# HTTP API - restrict if not using reverse proxy
ufw allow 8080/tcp

# Admin panel - localhost/VPN only
ufw deny 8081/tcp
ufw allow from 10.0.0.0/8 to any port 8081
ufw allow from 127.0.0.1 to any port 8081
```

### Reverse Proxy (Recommended)

Use Nginx or Traefik as reverse proxy for TLS termination:

```nginx
server {
    listen 443 ssl http2;
    server_name mail.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256;
    ssl_prefer_server_ciphers off;

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## Authentication Security

### Password Policy

Configure strong password requirements:

```yaml
security:
  min_password_length: 12
  require_password_complexity: true
  password_expiry_days: 90
  max_login_attempts: 5
  lockout_duration: 15m
```

### Two-Factor Authentication

Enable TOTP 2FA for all admin accounts:

```bash
# User enables 2FA via API
curl -X POST http://localhost:8080/api/auth/totp/setup \
  -H "Authorization: Bearer <token>"

# Verify with TOTP code
curl -X POST http://localhost:8080/api/auth/totp/verify \
  -H "Authorization: Bearer <token>" \
  -d '{"code": "123456"}'
```

### Account Lockout

Automatic account lockout is enabled by default after 5 failed attempts. Monitor for brute force:

```bash
# Check for failed login attempts
journalctl -u umailserver -g "authentication failure"
```

## TLS Configuration

### Minimum TLS Version

Configure minimum TLS version in config:

```yaml
tls:
  min_version: "1.2"
  cipher_suites:
    - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305
    - TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305
```

### Certificate Management

Use Let's Encrypt with auto-renewal:

```yaml
tls:
  acme:
    enabled: true
    email: admin@example.com
    provider: letsencrypt
    accept_tos: true
```

Or use custom certificates:

```yaml
tls:
  cert_file: /etc/umailserver/certs/server.crt
  key_file: /etc/umailserver/certs/server.key
```

## API Security

### Rate Limiting

API rate limiting is enabled by default:

```yaml
api:
  rate_limit: 100          # requests per minute
  burst_limit: 20          # burst capacity
  auth_rate_limit: 20      # login attempts per minute
```

### JWT Security

- Use strong secrets (32+ characters)
- Set appropriate token expiry
- Rotate secrets periodically

```yaml
security:
  jwt_secret: "your-32-char-minimum-secret-key-here"
  token_expiry: 24h
  refresh_token_expiry: 7d
```

### CSRF Protection

The API implements CSRF protection through:
1. JWT tokens (stateless CSRF protection)
2. Content-Type validation for API endpoints
3. Origin validation (configurable)

### Security Headers

The following security headers are automatically set by the API:

| Header | Value | Purpose |
|--------|-------|---------|
| X-Frame-Options | DENY | Prevents clickjacking |
| X-Content-Type-Options | nosniff | Prevents MIME sniffing |
| X-XSS-Protection | 1; mode=block | Legacy XSS protection |
| Referrer-Policy | strict-origin-when-cross-origin | Limits referrer info |
| Content-Security-Policy | default-src 'self'... | Prevents XSS/injection |
| Strict-Transport-Security | max-age=31536000... | Enforces HTTPS |

## Input Validation

### Email Validation

All email addresses are validated using RFC 5322 compliant validation:

```go
// Server-side validation
if err := ValidateEmail(email); err != nil {
    return fmt.Errorf("invalid email: %w", err)
}
```

### Message Size Limits

Configure maximum message sizes:

```yaml
smtp:
  max_message_size: 50MB
  max_recipients: 100

imap:
  max_fetch_size: 10MB
```

### Content Sanitization

HTML emails are sanitized to prevent XSS:

```yaml
security:
  sanitize_html: true
  allowed_tags: ["p", "br", "strong", "em", "a"]
  allowed_attributes: ["href"]
```

## Operational Security

### Logging

Enable security audit logging:

```yaml
logging:
  level: info
  audit_log: /var/log/umailserver/audit.log
  log_auth_events: true
  log_admin_actions: true
```

### Monitoring

Monitor for security events:

```yaml
alerting:
  enabled: true
  webhook_url: "https://hooks.slack.com/..."
  error_threshold: 5           # Alert on 5+ errors/minute
  failed_login_threshold: 10   # Alert on 10+ failed logins/minute
```

### Backup Security

Encrypt backups and secure backup storage:

```bash
# Encrypt backups
umailserver backup --encrypt --output backup.tar.gz.gpg

# Secure file permissions
chmod 600 backup.tar.gz.gpg
```

## Security Checklist

Before production deployment:

- [ ] JWT secret is 32+ random characters
- [ ] TLS 1.2+ configured with strong ciphers
- [ ] Admin panel restricted to localhost/VPN
- [ ] Rate limiting enabled
- [ ] File permissions set correctly
- [ ] Firewall rules configured
- [ ] Audit logging enabled
- [ ] Security headers enabled
- [ ] Input validation in place
- [ ] 2FA enabled for admin accounts
- [ ] Backups encrypted and secured
- [ ] Monitoring and alerting configured
- [ ] Fail2ban or similar configured for SSH

## Vulnerability Reporting

Report security vulnerabilities to: **security@umailserver.com**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

## Security Updates

Subscribe to security advisories:
- GitHub Security Advisories
- Mailing list: security-announce@umailserver.com

Update promptly when security patches are released.
