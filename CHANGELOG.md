# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-04-11

### Added

- **SMTP Server**
  - Full SMTP/ESMTP server with STARTTLS support (ports 25, 587, 465)
  - SPF, DKIM, DMARC authentication stages
  - ARC (Authentication Received Chain) support
  - Greylisting with auto-whitelist
  - RBL (Realtime Blacklist) checking
  - Heuristic spam scoring with Bayesian filter
  - Antivirus scanning integration (ClamAV)
  - SMTP pipeline stages with pluggable architecture

- **IMAP4rev1 Server**
  - Full IMAP4rev1 support (ports 143, 993)
  - Maildir++ storage backend (bbolt metadata + filesystem)
  - SORT and THREAD extensions
  - Idle (push notifications) support
  - ACL (Access Control List) support
  - Quota enforcement

- **POP3 Server**
  - POP3 server with TLS support (port 995)
  - APOP authentication support
  - Top command support

- **ManageSieve Server**
  - Sieve script management (port 4190)
  - RFC 5228 compliant script parsing and execution
  - Vacation auto-responder with date range support
  - Multiple filter scripts per account

- **REST API**
  - Full REST API at `/api/v1/`
  - JWT authentication with refresh tokens
  - Token blacklisting for immediate revocation
  - Admin panel endpoints (domains, accounts, aliases, queue)
  - User endpoints (mail, filters, sieve scripts)
  - SSE real-time notifications
  - OpenAPI/Swagger documentation

- **Web Interfaces**
  - React 19 webmail client at `/webmail/`
  - React 19 admin panel at `/admin/`
  - React 19 account management portal at `/account/`
  - Thunderbird/Outlook autoconfig at `/.well-known/autoconfig/`
  - Microsoft Autodiscover support

- **Database**
  - Embedded bbolt database for all persistence
  - Domain, account, alias, queue management
  - Database migration system with rollback support

- **Security**
  - Argon2id password hashing (OWASP recommended)
  - bcrypt fallback
  - TOTP 2FA support
  - Rate limiting (per-IP, per-user, global)
  - Input validation on all user inputs
  - XSS sanitization via DOMPurify
  - Audit logging for admin actions

- **Queue & Delivery**
  - Outbound mail queue with retry logic
  - MX connection pooling (configurable pool size)
  - DSN (Delivery Status Notification) support
  - Bounce message handling

- **Additional Protocols**
  - CalDAV calendar server (RFC 4791)
  - CardDAV contacts server (RFC 6352)
  - JMAP email API (RFC 8620)
  - MCP (Model Context Protocol) server for AI integration
  - WebSocket/WebPush notifications
  - Prometheus metrics endpoint

- **Operations**
  - Database backup with AES-256-GCM encryption
  - Backup verification via SHA256 manifest
  - Automated backup restore testing (weekly CI)
  - Fuzz testing for SMTP/IMAP parsers (24h CI)
  - Zero-downtime deployment with graceful drain
  - Health check endpoints (`/health`, `/health/ready`)
  - Frontend Vitest component tests

### Fixed

- Vacation auto-reply memory leak
- Queue retry logic edge cases
- Race conditions in concurrent handlers
- Security vulnerabilities from audit
- Windows build compatibility issues

### Security

- 17 security vulnerabilities patched
- Input validation on all API endpoints
- Path traversal protection in backup restore
- HTTPS-only cookie flags

## [0.0.0] - 2026-04-07

### Added

- Initial project setup
- Basic SMTP server skeleton
- Database schema design
