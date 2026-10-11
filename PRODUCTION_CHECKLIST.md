# uMailServer Production Readiness Checklist

## ✅ Core Functionality

| Component | Status | Notes |
|-----------|--------|-------|
| SMTP (Port 25, 465, 587) | ✅ | Full STARTTLS/TLS support |
| IMAP (Port 143, 993) | ✅ | Full STARTTLS/TLS support |
| POP3 (Port 995) | ✅ | TLS support |
| ManageSieve (Port 4190) | ✅ | Script management |
| HTTP API (Port 443) | ✅ | REST API + Web UIs (plain HTTP: terminate TLS in a reverse proxy) |
| Admin Panel (Port 8443) | ✅ | Localhost-only by default |
| MCP Server (Port 3000) | ✅ | Loopback by default; non-loopback bind requires an explicit `mcp.auth_token` |
| CalDAV/CardDAV | ✅ | Calendar/contacts sync |
| JMAP | ✅ | Modern email API |

## ✅ Security

| Feature | Status | Notes |
|---------|--------|-------|
| Password Hashing | ✅ | bcrypt by default; Argon2id supported |
| TOTP 2FA | ✅ | Time-based OTP |
| JWT Authentication | ✅ | HttpOnly cookie or Bearer token, token refresh/rotation, revocation list |
| Rate Limiting | ✅ | Per-IP and per-user; per-account login lockouts |
| SPF/DKIM/DMARC | ✅ | Full email auth stack |
| ARC | ✅ | Authentication chain |
| Input Validation | ✅ | All endpoints validated |
| Audit Logging | ✅ | Admin action logging |
| TLS 1.2/1.3 | ✅ | Modern TLS only |

## ✅ Documentation

| Document | Status |
|----------|--------|
| API Specification (OpenAPI 3.0.3) | ✅ |
| Architecture Guide | ✅ |
| Deployment Guide | ✅ |
| Performance Tuning | ✅ |
| Security Hardening | ✅ |
| Distributed Tracing | ✅ |
| Troubleshooting Guide | ✅ |
| Windows Test Results | ✅ |

## ✅ Testing

| Test Type | Coverage | Status |
|-----------|----------|--------|
| Unit Tests | 36 packages | ✅ PASS |
| Server Package | 73.6% | ✅ |
| API Package | 72.8% | ✅ |
| Auth Package | 70.5% | ✅ |
| Integration Tests | Key flows | ✅ PASS |
| Windows Local Test | Full stack | ✅ PASS |
| Docker Build | Multi-stage | ✅ PASS |

## ✅ Operations

| Feature | Status |
|---------|--------|
| Docker Support | ✅ Multi-stage Dockerfile |
| Health Checks | ✅ `/health`, `/health/ready` (web port); `/healthz` (metrics port) |
| Prometheus Metrics | ✅ `/metrics` on the metrics listener (127.0.0.1:8080 by default) |
| OpenTelemetry Tracing | ✅ OTLP/stdout/noop |
| Queue Management | ✅ Retry + backoff |
| Backup/Restore | ✅ CLI commands (stop the server for `backup`; `restore` extracts to `restore_temp/`) |
| Log Rotation | ✅ Built-in |
| Graceful Shutdown | ✅ Signal handling |

## ✅ Performance

| Feature | Status |
|---------|--------|
| Connection Pooling | ✅ SMTP/IMAP |
| Cache Metrics | ✅ SPF/DKIM/DMARC |
| Search Indexing | ✅ TF-IDF based |
| Rate Limiting | ✅ Multiple levels |
| Circuit Breaker | ✅ External services |

## 📋 Pre-Deployment Checklist

### Infrastructure
- [ ] Linux server (Ubuntu 22.04+ recommended)
- [ ] Static IP address
- [ ] Reverse DNS (PTR) record configured
- [ ] Firewall rules (ports 25, 465, 587, 143, 993, 443)
- [ ] Sufficient disk space (100GB+ recommended)
- [ ] RAM (4GB minimum, 8GB recommended)

### DNS Records
- [ ] A record: mail.example.com → server IP
- [ ] MX record: example.com → mail.example.com
- [ ] PTR record: IP → mail.example.com
- [ ] SPF record: v=spf1 mx ~all
- [ ] DKIM: Generate via umailserver
- [ ] DMARC: v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com
- [ ] Autoconfig: DNS SRV records

### TLS Certificates
- [ ] Let's Encrypt (automatic) OR
- [ ] Manual certificates configured
- [ ] TLS 1.2+ enforced
- [ ] HTTPS for webmail/REST API terminated by a reverse proxy (the web/admin listeners speak plain HTTP)
- [ ] Reverse proxy address listed in `http.trusted_proxies` (client IP for rate limits and audit log)

### Configuration
- [ ] Strong JWT secret (32+ chars)
- [ ] SMTP auth credentials configured
- [ ] Rate limits tuned for your traffic
- [ ] Spam filter thresholds set
- [ ] MCP kept on loopback, or `mcp.auth_token` set explicitly for a non-loopback bind
- [ ] Do not rely on DANE: outbound TLSA records are logged but not enforced by the stock server (MTA-STS `enforce` policies of recipient domains are honoured)
- [ ] Admin account created
- [ ] First domain added

### Monitoring
- [ ] Prometheus scraping configured
- [ ] Alert manager webhooks set
- [ ] Log aggregation configured
- [ ] Health checks monitoring

### Backup
- [ ] Automated backup schedule
- [ ] Backup storage configured
- [ ] Restore procedure tested (`umailserver restore` extracts to `restore_temp/`; copy into `data_dir` with the server stopped)
- [ ] Backups taken with the server stopped (`umailserver backup` refuses a live server)

### Upgrades
- [ ] Back up before upgrading; a database written by a newer binary is refused ("upgrade the binary"), so downgrades need a restore
- [ ] `umailserver db status` after upgrading; `SIGHUP` reloads only manual TLS certificates, all other config changes need `umailserver restart`

## 🚀 Deployment Commands

```bash
# Binary
docker run -d \
  --name umailserver \
  -p 25:25 -p 465:465 -p 587:587 \
  -p 143:143 -p 993:993 \
  -p 443:443 -p 8443:8443 \
  -v /data/umailserver:/var/lib/umailserver \
  -v /etc/umailserver:/etc/umailserver \
  umailserver:latest

# Docker Compose
docker-compose up -d
```

## ✅ Sign-off

| Person | Role | Date | Signature |
|--------|------|------|-----------|
| | Technical Lead | | |
| | Security Officer | | |
| | DevOps Lead | | |

## Version Information

- **Version:** v0.1.0
- **Commit / Build Date:** run `umailserver version` on the release binary
- **Go Version:** 1.25+
- **Node Version:** 20+

---

**Status:** ⚠️ BETA — the mail core is ready; see Known Limitations in README.md before deploying
