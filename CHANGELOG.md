# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Audit rounds 141-220 (user-visible behaviour)

Facts below come from the per-round commit messages (`git log`, PRs #61-#67);
each bullet is a behaviour an operator or client can observe.

**Rounds 141-150 (PR #61)**
- Config: `serve` requires an explicit, existing config file; unknown YAML keys are reported (`umailserver config check` fails on them); relative paths resolve against the config file; the accounts DB defaults to `<data_dir>/umailserver.db`; stricter validation.
- SMTP: `452` for a full mailbox, address case-insensitivity, per-stage timeout, per-session byte cap, `BINARYMIME`/`SMTPUTF8`, `VRFY`/`EXPN` handling.
- Outbound: MTA-STS `enforce` policies are enforced with no plaintext fallback (working resolver, policy cache, TLS-failure counters).
- Sieve: RFC 5228 compile-time strictness and the address/envelope/body/relational/regex/imap4flags/copy tests, with limits.
- API/DB: session revocation, a mail move endpoint, atomic account mutation, pagination; login names are normalised.
- IMAP: NFC mailbox names, rejected APPEND literals, `ESEARCH` `RETURN`.
- JMAP: upload GC, import quota, body values and header forms, patch validation. Webhooks/alerts: persistent webhook registry, signature timestamp, exponential backoff, redirect guard.
- CLI: `quickstart` prints the real DKIM record, no demo accounts; trash restore uses move.

**Rounds 151-160 (PR #62)**
- Per-username/per-account failed-login lockouts on SMTP AUTH, IMAP, POP3, ManageSieve, and Basic auth for CalDAV/CardDAV (bounded trackers).
- Storage store/delete serialised per user; expired TLS certificates are treated as invalid and the certs directory mode is tightened.
- TOTP enable/disable revoke sessions and attempts are limited. Webmail inbox/spam/detail use the real API.
- Queue: DSN header-injection fix, MX host de-duplication. Backup: streaming verify, partial backups are cleaned up, Maildir symlinks are skipped. Search/vacation/audit: stale-hit and header-injection fixes, secret redaction.

**Rounds 161-170 (PR #63)**
- API: TOTP fails closed, login timing equalised, password policy (8-128 chars, upper/lower/digit/special), tokens must carry `exp`.
- Blob deletion is reference-aware across storage/IMAP/POP3; vacation replies use the null envelope sender and share the rules manager.
- CLI/config: streaming encrypted backup, Dovecot import reports errors, warning when the config file holding credentials is group/world readable.
- JMAP query filters/sort/validation and `Email/set`; CalDAV/CardDAV href escaping and MOVE/COPY/DELETE fixes; DKIM/SPF hardening, bounded spam tokenisation; SSRF dial guard for alert/push/health, SSE close fixes.

**Rounds 171-180 (PR #64)**
- New REST mail endpoints: `/api/v1/mail/message`, `/flags`, `/draft`, `/move`, with pagination; CSRF host check on cookie-authenticated requests.
- IMAP: raw DEFLATE, serialised `EXPUNGE`, tag validation. POP3: maildrop lock, `TOP`; ManageSieve/POP3 connection caps.
- MCP: constant-time token compare. Autoconfig: domain validation. DB: alias/TOTP/revocation/cascade fixes. Admin/account web UIs: pagination, 401 sign-out, honest settings, password-policy errors. CLI: PID identity check before signalling, `O_EXCL` Maildir tmp files.

**Rounds 181-190 (PR #65)**
- Server: Sieve multi-folder delivery, ordered stop, MCP refuses a non-loopback bind without an explicit token, TLS reload hook, hosted-domain lookup for autoconfig.
- IMAP: per-session sequence view, `IDLE`, search limits. SMTP: header folding / From ambiguity, verdict-header stripping, greylisting by /24.
- DB: schema migration guard, atomic batch, backup, atomic queue claim. API: last-active-admin guard (`409`), `forward_to` validation, `Retry-After` on `429`, `Allow` on `405`.
- Storage: key escaping, UID exhaustion, INBOX protection, orphan GC. Rate limiter/metrics/health/webhook/push: caps, gauges, in-flight tracking, idempotency. Docs aligned with code (R190).

**Rounds 191-200 (PR #66)**
- SMTP: AV tag header, per-IP connection cap, RBL lookups in parallel with cache, `Received` always added, strict `BDAT`/`STARTTLS`.
- `SIGHUP` reloads manual TLS certificates only (other config changes need `umailserver restart`); a database written by a newer binary is refused with an upgrade message; atomic queue claim.
- API: account delete cascades to mailbox data, per-copy qualified message ids (`<hash>_<uid>_<base64url(mailbox)>`) for duplicate content, draft Cc/Bcc, `507` when the REST send/draft would exceed quota; health/auth/autoconfig/`Allow` fixes. Audit/log rotation fixes. In-transaction last-admin guard and user purge.
- POP3 double quota release fixed; `IDLE`/SSE are notified on delivery.

**Rounds 201-210 (PR #67)**
- SMTP: graceful stop drains in-flight `DATA`, atomic global connection cap, TLS handshake deadline; `smtp.max_connections_per_ip` and `require_tls` honoured; persisted alert cooldowns.
- IMAP: idle timeout, parent `LIST` levels, literal/`ENABLE`/AUTH cancel fixes. ARC/SPF/DKIM/DANE/MTA-STS hardening; DANE/TLSA data counts only when DNSSEC-authenticated (the stock server installs no validation hook, so DANE stays advisory).
- Search/storage: atomic thread reservation, MIME-aware indexing. API: server-side `flag=flagged` and `unread=1` filters on the mail list, hosted-domain autodiscover, audit client IP taken from `X-Forwarded-For` only for `http.trusted_proxies`, SSE session revalidation, DAV purge on account delete.

**Rounds 211-220**
- See the repository ledgers in `.temp_files/ledger_r2*.md`; round 220 is a documentation round (below).

### Documentation (round 220)

- `api/openapi.yaml`, `api/swagger.*` and `api/README.md` rewritten from the real route table (`/api/v1/...`); `docs/api-reference.md` corrected (login/refresh responses, string error bodies, `X-Total-Count` pagination, mail list filters and qualified ids, `409`/`429`+`Retry-After`/`507`, queue retry is `POST /admin/queue/{id}`, no `/domains/{d}/dns` route).
- Ports and health: metrics + `/healthz` live on the metrics listener (`127.0.0.1:8080`); `/health` and `/health/ready` on the web port; there is no `/health/live`. The web, API and admin listeners are plain HTTP (TLS is terminated by a reverse proxy).
- CLI docs match the real subcommands (`serve --config`, no `queue show`, `db check`, `compact-db`, `setup`, `backup create`); `backup` refuses a live server and `restore` extracts to `restore_temp/`; migration docs reflect the `imap`/`dovecot`/`mbox` importers (MBOX destination comes from the file name).
- IMAP capabilities list corrected (`CONDSTORE`/`QRESYNC`/IMAP4rev2 are not advertised); DANE documented as advisory (never enforced by the stock server); MTA-STS is outbound enforcement only (RFC 8461).
- `dev.yaml.example` / `prod.yaml.example`: removed keys the server ignores and fixed the default-port clash with the metrics listener.

### Added

- **Distributed Tracing**
  - OpenTelemetry integration with OTLP/stdout/noop exporters
  - Span context propagation across SMTP/IMAP/HTTP boundaries
  - Configurable sampling rate (0.0 to 1.0)
  - Jaeger/Tempo compatibility

- **Cache Metrics**
  - SPF cache hit/miss counters
  - DKIM public key cache with metrics
  - DMARC policy cache with metrics
  - Configurable TTL (default: 5 minutes)
  - LRU eviction with max size limits

- **Documentation**
  - Complete OpenAPI 3.0.3 specification
  - Distributed tracing setup guide
  - Performance tuning guide for production

### Changed

- **Architecture refactor**: Split oversized monolith files into focused per-subsystem files
  - `api/server.go`: 2550→892 lines (14 focused files)
  - `server/server.go`: 1689→284 lines (18 focused files)

### Fixed

- Duplicate HTTP fetch logic in `cmd/umailclient/main.go` extracted to `fetchAndPrint()` helper
- Error handling in `api/filters.go` - `saveFilter` error now properly logged instead of ignored

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
