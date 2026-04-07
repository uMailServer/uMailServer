# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-04-03

### Added - Production Readiness Release

#### Core Email Protocols
- SMTP server with STARTTLS, PIPELINING, CHUNKING, SMTPUTF8
- SMTPS (port 465) and Submission (port 587) with AUTH
- IMAP4rev1 with IDLE, CONDSTORE, ENABLE extensions
- POP3 with UIDL, TOP, SASL support

#### Authentication & Security
- bcrypt password hashing with configurable cost
- JWT authentication with HS256 and expiry/refresh
- TOTP 2FA (RFC 6238) support
- Rate limiting (100 req/min authenticated, 20 req/min unauthenticated)
- Account lockout (5 failed attempts = 15min lockout)
- Brute force protection with IP tracking
- Security headers (CSP, HSTS, X-Frame-Options, X-Content-Type-Options)
- CSRF protection via Content-Type validation

#### Email Security
- SPF verification (RFC 7208)
- DKIM verification (RFC 6376)
- DMARC evaluation (RFC 7489)
- ARC support (RFC 8617)
- DANE support (RFC 6698)
- Greylisting anti-spam
- RBL/DNSBL checking
- TF-IDF spam scoring
- ClamAV antivirus integration

#### Storage & Persistence
- Maildir++ format storage
- bbolt embedded database
- Full-text search with TF-IDF indexing
- Queue with 4 priority levels and retry logic
- Backup/restore with SHA256 integrity

#### TLS & Encryption
- TLS 1.2/1.3 with modern cipher suites
- ACME/Let's Encrypt auto-provisioning
- Custom certificate support
- Certificate expiry monitoring

#### Observability
- Prometheus metrics endpoint (100% coverage)
- Health checks (/health, /live, /ready)
- Structured logging with rotation
- OpenTelemetry distributed tracing
- Alert system (webhook + email)
- Resource monitoring (disk, memory, goroutines)

#### Reliability
- Circuit breaker pattern
- Connection draining for graceful shutdown
- Resource limits (memory, goroutines, connections)
- goroutine leak prevention
- Panic recovery

#### Management & API
- REST API with OpenAPI 3.0 specification
- Admin dashboard (React-based)
- CLI tools (backup, diagnostics, migration)
- MCP server for AI assistants
- WebSocket real-time updates
- Webhook event notifications

#### Configuration
- YAML configuration with validation
- Environment variable overrides
- Port conflict detection
- Size/duration type parsing

#### Testing
- 25 packages with ~87% average coverage
- Integration tests for mail flows
- Performance benchmarks
- Race detection testing
- Cross-platform support (Windows/Linux/macOS)

#### Documentation
- Deployment guide (DEPLOYMENT.md)
- Security policy (SECURITY.md)
- Security hardening guide
- API documentation (OpenAPI)
- Production readiness checklist

### Performance Benchmarks
| Operation | Latency |
|-----------|---------|
| Maildir Deliver | ~646μs |
| Maildir Fetch | ~40μs |
| Account Lookup | ~4.4μs |
| Password Verify | ~38ms (bcrypt) |
| SMTP Parse | ~9ns |

### Coverage Summary
- metrics: 100%
- tracing: 100%
- av: 98.8%
- search: 97.9%
- tls: 95.9%
- auth: 94.9%
- cli: 92.7%
- store: 92.4%
- smtp: 91.0%
- websocket: 91.7%
- Average: ~87%

## [0.9.0] - 2026-03-15

### Added
- Initial beta release
- Basic SMTP/IMAP/POP3 support
- Simple authentication
- File-based storage

## [0.8.0] - 2026-02-28

### Added
- Alpha release
- Proof of concept implementation

---

## Versioning Guide

- **MAJOR** - Breaking changes, protocol updates
- **MINOR** - New features, backwards compatible
- **PATCH** - Bug fixes, security patches

## Support

- Documentation: https://docs.umailserver.io
- Issues: https://github.com/umailserver/umailserver/issues
- Security: security@umailserver.com
