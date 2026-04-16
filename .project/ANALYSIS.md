# Project Analysis Report

> Auto-generated comprehensive analysis of uMailServer
> Generated: 2026-04-16
> Analyzer: Claude Code — Full Codebase Audit
> Verified: `go build ./...` passes, `go test ./...` 37/37 packages pass

---

## 1. Executive Summary

uMailServer is a **single-binary monolith email server written in Go** implementing SMTP (inbound/outbound/submission), IMAP4rev1/4rev2, POP3, and embedded React frontends. It provides spam filtering (SPF/DKIM/DMARC/ARC/RBL/greylisting), TLS via ACME/Let's Encrypt, JWT auth with Argon2id/bcrypt, TOTP 2FA, Sieve mail filtering, CalDAV/CardDAV, JMAP, MCP AI integration, and React-based webmail/admin/account portal — all embedded via `embed.FS` into a single binary.

**Key Metrics:**
| Metric | Value |
|--------|-------|
| Go Source Files | 340 |
| Go LOC (estimated) | ~175,000 (including generated/test files) |
| Go Source LOC (excluding tests/vendor) | ~100,000 |
| Test Files | 198 |
| Packages Tested | 37 |
| Test Status | **ALL PASS** (37/37 packages) |
| Build Status | **PASSES** (`go build ./...`) |
| Direct Go Dependencies | 9 |
| Frontend Projects | 3 (webmail, admin, account) |

**Overall Health Assessment: 8/10**

Strengths: Comprehensive RFC-compliant email server, extensive test coverage (~78% avg), modern Go 1.25, clean package structure (after refactor), security primitives well-implemented.

Concerns: S/MIME and OpenPGP are placeholder implementations (XOR encryption, not real crypto), distributed tracing has no actual spans, CalDAV/CardDAV stubs, JMAP minimal.

---

## 2. Architecture Analysis

### 2.1 High-Level Architecture

**Type:** Modular Monolith — Single binary, multiple protocol servers (SMTP/IMAP/POP3/HTTP/MCP), shared storage via Maildir++ and dual bbolt databases.

**Startup Flow:**
```
main.go → config.Load() → server.New(cfg) → srv.Start()
```

**Init Sequence:** DB (bbolt) → MessageStore (Maildir++) → TLS Manager → Queue → Mailstore → SMTP (port 25) → Submission SMTP (587/465) → IMAP → POP3 → MCP → HTTP API

**Build Chain:** `make build` → `build-web` (npm builds webmail/, web/admin/, web/account/ to `dist/`) → `go build`

### 2.2 Package Structure

| Package | LOC (est.) | Assessment |
|---------|------------|------------|
| `api` | ~900 | ✅ Refactored (2026-04-12): split into 14 focused files |
| `server` | ~300 | ✅ Refactored (2026-04-12): split into 18 focused files |
| `smtp` | ~1,200 | Good — pipeline stages, session handling |
| `auth` | ~1,500 | ⚠️ SPF bug fixed, DKIM fixed, but S/MIME/OpenPGP are stubs |
| `queue` | ~1,100 | Good — bounded worker pool, retry logic |
| `storage` | ~400 | Good — Maildir++ + bbolt |
| `db` | ~500 | Good — bbolt persistence |
| `config` | ~700 | Good — YAML loading, validation |
| `imap` | ~800 | Moderate — full IMAP4rev1 |
| `pop3` | ~300 | Moderate — RFC 1939 compliant |
| `ratelimit` | ~489 | Good |
| `tls` | ~400 | Good — ACME/Let's Encrypt |
| `spam` | ~400 | ✅ Bayesian fully implemented (needs training data) |
| `av` | ~200 | ✅ ClamAV TCP INSTREAM (needs daemon) |
| `metrics` | ~200 | Good — Prometheus |
| `health` | ~300 | Good |
| `logging` | ~200 | Good — structured JSON |
| `tracing` | ~100 | ⚠️ Stub — no actual spans |
| `sieve` | ~400 | Good — RFC 5228 |
| `caldav` | ~400 | ❌ Stub storage only |
| `carddav` | ~400 | ❌ Stub storage only |
| `jmap` | ~300 | 🟡 Minimal — basic handlers exist |
| `mcp` | ~300 | 🟡 Minimal — JSON-RPC server exists |
| `vacation` | ~200 | ✅ Wired to Sieve |
| `webhook` | ~200 | ✅ Wired — events on mail received, delivery, auth |
| `autoconfig` | ~300 | Good — Thunderbird/Outlook |
| `alert` | ~100 | ✅ Wired — TLS expiry + queue backlog checks |
| `push` | ~100 | ✅ Wired — SendNewMailNotification from deliverLocal |
| `circuitbreaker` | ~200 | ✅ Wired — mxBreaker in queue delivery |
| `audit` | ~200 | ✅ Wired — login/logout/account events |

### 2.3 Dependency Analysis

**Go Dependencies (go.mod):**
| Dependency | Version | Purpose | Maintenance |
|------------|---------|---------|-------------|
| `go.etcd.io/bbolt` | v1.4.3 | Embedded KV store | Active |
| `github.com/emersion/go-imap` | v1.2.1 | IMAP4rev1 server | Active |
| `github.com/go-ldap/ldap/v3` | v3.4.13 | LDAP auth | Active |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | JWT tokens | Active |
| `github.com/google/uuid` | v1.6.0 | UUID generation | Active |
| `github.com/miekg/dns` | v1.1.72 | DNS queries (SPF/DKIM/DMARC) | Active |
| `golang.org/x/crypto` | v0.50.0 | bcrypt/argon2/ed25519 | Active |
| `google.golang.org/grpc` | v1.80.0 | gRPC (JMAP) | Active |
| `gopkg.in/yaml.v3` | v3.0.1 | Config parsing | Active |
| `go.opentelemetry.io/otel*` | v1.43.0 | Distributed tracing | Active |
| `github.com/SherClockHolmes/webpush-go` | v1.4.0 | WebPush notifications | Active |

**Dependency Hygiene:** Good — no unused deps, no known CVEs, all actively maintained.

### 2.4 API & Interface Design

**HTTP API Endpoints (internal/api/):**
- Auth: `/api/v1/auth/login`, `/api/v1/auth/refresh`, `/api/v1/auth/logout`
- Mail: `/api/v1/mail/{folder}`, `/api/v1/mail/send`, `/api/v1/mail/search`
- Domains: `/api/v1/domains`
- Accounts: `/api/v1/accounts`
- Aliases: `/api/v1/aliases`
- Queue: `/api/v1/queue`
- Filters: `/api/v1/filters`
- Vacation: `/api/v1/vacation`
- Push: `/api/v1/push`
- Threads: `/api/v1/threads`
- Health: `/health`, `/health/live`, `/health/ready`
- Metrics: `/metrics`

**SMTP Ports:** 25 (MX), 587 (Submission), 465 (Implicit TLS)
**IMAP Ports:** 143 (STARTTLS), 993 (Implicit TLS)
**POP3 Ports:** 110 (STARTTLS), 995 (Implicit TLS)
**Other:** 4190 (ManageSieve), 8443 (Admin), 3000 (MCP)

---

## 3. Code Quality Assessment

### 3.1 Go Code Quality

**Strengths:**
- Minimal, high-quality deps — all mature and well-maintained
- Comprehensive test suite — 37 packages, all passing
- Modern Go 1.25 — generics, `any`, `slices`, `maps` from stdlib
- Structured logging via `log/slog` with JSON, rotation
- Path security — `validatePathComponent()` prevents traversal
- Atomic file writes — queue uses temp-file + `Rename` + `Sync`
- Graceful shutdown — `sync.Once` for stop, drain support
- gofmt-clean
- Security primitives — bcrypt/argon2id, JWT with rotation, brute-force protection, rate limiting
- DKIM signing — RSA-SHA256 + Ed25519-SHA256

**Issues Found:**
1. **`regexp.MustCompile` in hot paths** — dkim.go has compiled regexes per-message (should be package-level)
2. **LDAP TLS SkipVerify configurable** — `ldap.go:162,172` with `#nosec G402` for self-signed certs
3. **RSA-2048 hardcoded assumption** — `ldap.go:180-185` assumes encrypted key is exactly 256 bytes
4. **String comparison for TLSA data** — `dane.go:313` uses direct string comparison instead of constant-time

### 3.2 Concurrency & Safety

**Good Patterns:**
- Bounded worker pool in queue (20 concurrent)
- Context cancellation propagation
- Mutex protection on shared state (greylist, vacation cache, etc.)
- Circuit breaker on MX delivery

**Issues:**
1. **GreylistStage unbounded map** — grows to 100,000 before 50% cleanup
2. **Vacation cache unbounded** — no max size or eviction
3. **Sieve regex cache unbounded** — no size limit or eviction
4. **ReDoS detection weak** — `sieve/interpreter.go:155-160` can be bypassed

### 3.3 Security Assessment

**Good:**
- bcrypt/argon2id password hashing
- JWT with rotation support
- Brute-force protection
- SPF/DKIM/DMARC validation pipeline
- Path traversal protection
- LDAP SSRF protection
- Bounded DNS lookup limits

**Concerns:**
- ❌ **S/MIME stub** — `auth/smime.go` uses XOR encryption, not AES
- ❌ **OpenPGP stub** — `auth/openpgp.go` uses XOR with demo key
- 🟡 **ReDoS in Sieve** — can be bypassed with nested quantifiers
- 🟡 **JWT pruning lexicographic bug** — `server_admin.go:27-45` string sort vs numeric

---

## 4. Testing Assessment

| Metric | Value |
|--------|-------|
| Packages tested | 37 |
| Packages passing | 37 |
| Test files | 198 |
| Coverage (API package) | ~88.6% |
| Coverage (average) | ~77.9% |
| Fuzzing CI | Present |
| Frontend Vitest | Yes |
| Integration tests | Basic (mailflow_test.go) |
| Load tests | None |

**Observation:** `make test` runs 37 packages in short mode. `cli` package tests take ~8s (fixed from ~71s with `testing.Short()` skips).

---

## 5. Specification vs Implementation Gap Analysis

### 5.1 Feature Completion Matrix

| Feature | Spec Status | Implementation Status | Notes |
|---------|-------------|----------------------|-------|
| SMTP (MX/Submission) | ✅ | ✅ Complete | Full RFC 5321 + ESMTP |
| IMAP4rev1 | ✅ | ✅ Complete | RFC 3501 + extensions |
| POP3 | ✅ | ✅ Complete | RFC 1939 |
| SPF Verification | ✅ | ✅ Complete | RFC 7208 |
| DKIM Signing/Verification | ✅ | ✅ Complete | RSA-SHA256 + Ed25519 |
| DMARC Evaluation | ✅ | ✅ Complete | RFC 7489 |
| ARC Validation & Sealing | ✅ | ✅ Complete | RFC 8617 |
| DANE/TLSA | ✅ | ✅ Complete | RFC 6698 |
| MTA-STS | ✅ | ✅ Complete | RFC 6711 |
| Greylisting | ✅ | ✅ Complete | Anti-spam |
| RBL/DNSBL | ✅ | ✅ Complete | Multiple servers |
| Spam Scoring | ✅ | ✅ Complete | Heuristic + RBL |
| Bayesian Filter | ✅ | ✅ Implemented | Robinson-Fisher, needs training |
| ClamAV Integration | ✅ | ✅ Implemented | TCP INSTREAM, needs daemon |
| ACME/Let's Encrypt | ✅ | ✅ Complete | Auto-renewal |
| JWT Auth | ✅ | ✅ Complete | HS256 + rotation |
| Argon2id/bcrypt | ✅ | ✅ Complete | Configurable cost |
| TOTP 2FA | ✅ | ✅ Complete | RFC 6238 |
| Rate Limiting | ✅ | ✅ Complete | Per-IP/user/global |
| Brute-force Protection | ✅ | ✅ Complete | Connection-level |
| Sieve Filtering | ✅ | ✅ Complete | RFC 5228 |
| ManageSieve | ✅ | ✅ Complete | Port 4190 |
| Vacation Auto-responder | ✅ | ✅ Wired | SieveStage callback |
| Queue (outbound) | ✅ | ✅ Complete | Exponential backoff |
| DSN/MDN | ✅ | ✅ Complete | RFC 3461/3798 |
| MX Connection Pooling | ✅ | ✅ Complete | 10 conns per MX |
| Autoconfig/Autodiscover | ✅ | ✅ Complete | Thunderbird/Outlook |
| Prometheus Metrics | ✅ | ✅ Complete | Full instrumentation |
| Health Checks | ✅ | ✅ Complete | DB/queue/disk/TLS |
| Distributed Tracing | ✅ | ⚠️ Stub | OTel initialized, no spans |
| Webhook Events | ✅ | ✅ Wired | mail.received, delivery, auth |
| Push Notifications | ✅ | ✅ Wired | SendNewMailNotification |
| Alert Manager | ✅ | ✅ Wired | TLS expiry + queue backlog |
| CalDAV Server | ✅ | ❌ Stub | Storage only, no HTTP handlers |
| CardDAV Server | ✅ | ❌ Stub | Storage only, no HTTP handlers |
| JMAP | ✅ | ✅ Implemented | RFC 8620 surface + change journal for incremental sync |
| MCP Server | ✅ | 🟡 Partial | JSON-RPC, basic tools |
| S/MIME | ✅ | ❌ Stub | XOR encryption, not AES |
| OpenPGP | ✅ | ❌ Stub | XOR with demo key |
| Full-text Search | ✅ | ✅ Implemented | TF-IDF, not wired to webmail |
| Account Portal | ✅ | ✅ Working | React + Zustand + TanStack |
| Webmail | ✅ | ✅ Working | React 19 + Tailwind v4 |
| Admin Panel | ✅ | ✅ Working | React 19 + shadcn + Recharts |

### 5.2 Critical Deviations

1. **S/MIME not real crypto** — `auth/smime.go` uses XOR for content encryption, not AES-GCM despite RSA OAEP for session key. README claims "S/MIME (RFC 8551) support" — this is misleading.

2. **OpenPGP not real crypto** — `auth/openpgp.go` uses XOR with hardcoded demo key. README claims "OpenPGP (RFC 3156) support" — this is misleading.

3. **CalDAV/CardDAV HTTP handlers missing** — storage exists but no HTTP endpoints at `/dav/calendars/` or `/dav/contacts/`.

4. ~~**JMAP incomplete**~~ ✅ FIXED — full RFC 8620 surface (Mailbox/Email/Thread/Identity get/query/set/changes/queryChanges, Email/import, SearchSnippet/get) plus a per-user change journal in `storage.Database` powering incremental sync.

5. **Distributed tracing has no spans** — `internal/tracing/tracing.go` initializes correctly but no code creates spans.

6. **Full-text search not wired to webmail** — `search` package works but webmail uses basic folder search, not TF-IDF.

---

## 6. Performance & Scalability

### 6.1 Performance Patterns

**Good:**
- MX connection pooling (reuses connections to MX hosts)
- Maildir++ with subdirectory sharding (messageID[:2]/messageID[2:4]/)
- In-memory rate limiting with periodic bbolt persistence
- SPF/DKIM/DMARC caching (TTL-based, SPF TTL now configurable)
- Bounded concurrent delivery (20 workers)

**Concerns:**
1. **Body canonicalization allocations** — dkim.go uses `strings.Split` + `regexp.ReplaceAllString` + `strings.Builder` per message

### 6.2 Scalability Assessment

- **Horizontal scaling:** Not possible — bbolt is single-node, no clustering
- **State management:** Single-node only, sticky sessions required
- **Queue:** Single-node, no distributed processing
- **Connection limits:** Per-IP configurable, no global limit

---

## 7. Technical Debt Inventory

### 🔴 Critical (blocks production)

| Item | Location | Description | Status |
|------|----------|-------------|--------|
| S/MIME XOR encryption | `auth/smime.go:197-201,280-284` | XOR is not crypto | ✅ FIXED (AES-256-GCM) |
| OpenPGP XOR encryption | `auth/openpgp.go:229-232` | XOR with demo key | ✅ FIXED (AES-256-GCM) |
| ReDoS in Sieve | `sieve/interpreter.go:155-160` | `isSuspiciousPattern()` bypassed | ✅ FIXED |

### 🟡 Important (should fix before v1.0)

| Item | Location | Description | Status |
|------|----------|-------------|--------|
| Distributed tracing spans | `tracing/` | OTel spans wired to SMTP/IMAP/auth | ✅ Wired |
| CalDAV HTTP handlers | `caldav/` | Storage exists, RFC 4791 compliance | ⚠️ Partial |
| CardDAV HTTP handlers | `carddav/` | Storage exists, RFC 6352 compliance | ⚠️ Partial |
| JMAP incomplete | `jmap/`, `storage/changes.go` | Full RFC 8620 surface + change journal driving Mailbox/Email/Thread `*/changes` | ✅ FIXED |
| Greylist bounded cache | `smtp/pipeline.go` | Max 50K entries with LRU eviction | ✅ FIXED |
| Vacation bounded cache | `sieve/manager.go` | Max 10K entries with LRU eviction | ✅ FIXED |
| Sieve regex bounded cache | `sieve/interpreter.go:106-120` | Max 1000 entries with LRU eviction | ✅ FIXED |
| JWT pruning lexicographic | `server_admin.go:27-45` | Uses numeric timestamp comparison | ✅ FIXED |
| LDAP connection pooling | `auth/ldap.go`, `auth/ldap_pool.go` | Bounded pool (default 10) wrapping `Authenticate`/`GetUser` | ✅ FIXED |
| Full-text search wiring | `search/` | API wired, webmail integration pending | ⚠️ Partial |

### 🟢 Minor (nice to fix)

| Item | Location | Description | Fix |
|------|----------|-------------|-----|
| `regexp.MustCompile` in hot path | `dkim.go:451,535` | Per-message compilation | Package-level `var` |
| SPF cache TTL configurable | `auth/spf.go` | `security.spf_cache_ttl` in config | ✅ FIXED |
| DNSSEC not enforced in DANE | `auth/dane.go:429` | `ValidateWithDNSSEC()` exists but not called | Call it or remove comment |
| Constant-time TLSA comparison | `auth/dane.go:313` | Direct string comparison | Use `subtle.ConstantTimeCompare` |
| MX pool liveness Noop | `queue/manager.go:450` | Extra SMTP round-trip | Already fixed RSET |
| Account portal build | ✅ FIXED | Added to `make build-web` | N/A |
| API server split | ✅ FIXED | 2550→892 lines, 14 files | N/A |
| Server split | ✅ FIXED | 1689→284 lines, 18 files | N/A |
| signRSA nil hash | ✅ FIXED | `rand.Reader` added | N/A |
| Queue thundering herd | ✅ FIXED | Bounded worker pool | N/A |

---

## 8. Metrics Summary

| Metric | Value |
|--------|-------|
| Total Go Files | 340 |
| Total Go LOC (est.) | ~100,000 (source only) |
| Test Files | 198 |
| Test Coverage (API) | ~88.6% |
| Test Coverage (avg) | ~77.9% |
| External Go Dependencies | 9 direct, 17 indirect |
| Frontend Dependencies | React 19, Tailwind v4/v3, etc. |
| API Endpoints | ~30+ |
| Spec Feature Completion | ~85% |
| Overall Health Score | 8/10 |

---

## 9. Security Report Reference

Comprehensive security audit already performed — see `./security-report/` directory:
- `SECURITY-REPORT.md` — Executive summary
- `sc-*.md` files — Detailed findings by category
- `verified-findings.md` — Confirmed issues

Key findings from security audit:
- ✅ No SQL injection (no SQL used)
- ✅ No command injection
- ✅ Path traversal protection in place
- ✅ XSS concerns in email rendering addressed via DOMPurify
- ✅ CSRF protection active
- ✅ Rate limiting on auth endpoints
- ⚠️ S/MIME/OpenPGP are stubs (known)
- ⚠️ ReDoS in Sieve regex (known)