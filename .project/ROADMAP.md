# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-16
> This roadmap prioritizes work needed to bring the project to production quality.

---

## Current State Assessment

uMailServer is a **production-ready single-binary email server** for v0.1.0. Core protocols (SMTP/IMAP/POP3) are well-implemented with proper RFC compliance. Security features (SPF/DKIM/DMARC validation, JWT auth, rate limiting, brute-force protection) are solid. Build and tests pass cleanly across 37/37 packages.

**What's Working Well:**
- Core email protocols (SMTP/IMAP/POP3) fully RFC compliant
- Security primitives (TLS, auth, rate limiting, brute-force protection) solid
- S/MIME + OpenPGP now use real AES-256-GCM crypto (Phase 1 complete)
- Comprehensive test coverage (~78% average, ~86.7% API)
- Architecture refactored (api/server.go: 2550→892 lines, server/server.go: 1689→284 lines)
- Webhook/alert/push systems wired
- Sieve vacation wired
- Circuit breaker wired for MX delivery
- DMARC reporting implemented
- CalDAV/CardDAV HTTP handlers fully implemented (PROPFIND/REPORT/PUT/GET/DELETE/MKCALENDAR/MKCOL/PROPPATCH/MOVE/COPY)
- JMAP RFC 8620 methods wired (Mailbox/Email/Thread/Identity get/query/set/changes/queryChanges, Email/import, SearchSnippet/get)
- Distributed tracing spans wired into SMTP, IMAP, and HTTP handlers
- All caches bounded with LRU eviction (greylist 50K, vacation 10K, sieve regex 1K)
- JWT pruning bug fixed (numeric timestamp comparison)
- SPF cache TTL configurable via `security.spf_cache_ttl`

**Key Remaining Issues:** none — all critical and polish items closed. Future enhancements live in "Beyond v1.0" below.

---

## Phase 1: Critical Security Fixes ✅ COMPLETE

### 1.1 Replace S/MIME XOR with Real Crypto ✅ DONE

**File:** `internal/auth/smime.go`

XOR content encryption replaced with AES-256-GCM. RSA OAEP still wraps the session key.

### 1.2 Replace OpenPGP XOR with Real Crypto ✅ DONE

**File:** `internal/auth/openpgp.go`

Hardcoded XOR demo key replaced with AES-256-GCM symmetric encryption.

### 1.3 Fix Sieve ReDoS Vulnerability ✅ DONE

**File:** `internal/sieve/interpreter.go:155-160`

`isSuspiciousPattern()` extended to detect adjacent quantifier patterns like `(a+)+`.

---

## Phase 2: Protocol Completion ✅ COMPLETE

### 2.1 CalDAV HTTP Handlers ✅ DONE

**File:** `internal/caldav/server.go`

All major WebDAV/CalDAV verbs implemented: OPTIONS, PROPFIND, REPORT, PUT, GET, DELETE, MKCALENDAR, MKCOL, PROPPATCH, MOVE, COPY. Calendar-query REPORTs return events from storage.

### 2.2 CardDAV HTTP Handlers ✅ DONE

**File:** `internal/carddav/server.go`

All major WebDAV/CardDAV verbs implemented with full vCard CRUD, addressbook-query REPORT, and PROPPATCH for collection metadata.

### 2.3 JMAP Implementation ✅ DONE

**File:** `internal/jmap/handlers.go`, `internal/storage/changes.go`

Implemented per RFC 8620: Mailbox/Email/Thread/Identity `get`, `query`, `set`, `changes`, `queryChanges`, plus `Email/import`, `SearchSnippet/get`. `Mailbox/changes`, `Email/changes`, and `Thread/changes` now read from a per-user change journal (`storage.Database.RecordChange`/`GetChangesSince`) hooked into every mailbox/message mutation, so clients can sync incrementally instead of re-querying. State tokens are bbolt sequence numbers; entries are bounded by 30-day retention and 50K per user.

### 2.4 Distributed Tracing Spans ✅ DONE

**File:** `internal/tracing/tracing.go`, `internal/tracing/http.go`, `internal/smtp/pipeline.go`, `internal/queue/manager.go`, `internal/api/tracing.go`, `internal/jmap/server.go`, `internal/caldav/server.go`, `internal/carddav/server.go`, `internal/webhook/manager.go`

OTel provider wired to SMTP command-level (AUTH/DATA in `session.go`), IMAP commands (authenticate/select/append/expunge/search/fetch/store), POP3 commands (`pop3.<COMMAND>` server-kind with auth.success on PASS), ManageSieve commands (`managesieve.<COMMAND>` with auth.success on AUTHENTICATE), MCP method dispatch (`mcp.<method>` with `mcp.tool` attribute on `tools/call`), per-stage SMTP pipeline (`smtp.pipeline.<stage>` child spans with enriched `smtp.spf.*`, `smtp.dkim.*`, `smtp.dmarc.*`, `smtp.arc.result`, `smtp.spam.score` attributes), outbound queue delivery (`queue.deliver` + per-MX `queue.deliver.mx`), JMAP method dispatch (`jmap.<Method/name>` spans), webhook delivery (`webhook.deliver` client-kind spans with id/url/event/attempts/success), and a shared `tracing.HTTPMiddleware` powers `http.<METHOD>` / `caldav.<METHOD>` / `carddav.<METHOD>` server-kind spans with W3C trace context extraction, status_code recording, and 4xx/5xx → error status. All sites short-circuit when the provider is nil or disabled.

---

## Phase 3: Hardening (Week 4-6)

### 3.1 Fix Unbounded Cache Growth ✅ DONE

**Files:** `internal/smtp/pipeline.go` (greylist), `internal/sieve/manager.go` (vacation), `internal/sieve/interpreter.go` (regex)

Added LRU eviction with max size:
- Greylist: max 50,000 entries (reduced from 100,000)
- Vacation cache: max 10,000 entries
- Sieve regex cache: max 1,000 entries

### 3.2 Fix JWT Pruning Lexicographic Bug ✅ DONE

**File:** `internal/api/server_admin.go:27-48`

`kid` is now parsed as a numeric timestamp via `strconv.ParseInt` to find the truly oldest key.

### 3.3 Add LDAP Connection Pool ✅ DONE

**Files:** `internal/auth/ldap.go`, `internal/auth/ldap_pool.go`, `internal/auth/ldap_pool_test.go`

Added a bounded pool (default 10, configurable via `ldap.max_connections`) that amortizes TLS/TCP setup across `Authenticate` and `GetUser` calls. The pool drains stale conns on acquire, discards conns left in unknown state mid-bind, and closes idle conns on shutdown via `LDAPClient.Close()`.

### 3.4 Make SPF Cache TTL Configurable ✅ DONE

**File:** `internal/auth/spf.go`, `internal/config/config.go`, `internal/server/server_smtp.go`

Added `security.spf_cache_ttl` (Duration, defaults to 5m). `SPFChecker.SetCacheTTL` is wired in `startSMTP`.

---

## Phase 4: Missing Features ✅ COMPLETE

### 4.1 Wire Full-Text Search to Webmail ✅ DONE

**Files:** `internal/api/server_search.go`, `webmail/src/pages/search.tsx`, `webmail/src/utils/api.ts`

`webmail/src/pages/search.tsx` calls `API.search(query)` → `GET /api/v1/search?q=...` → `searchSvc.Search()` (TF-IDF). The search UI also has recent-searches localStorage and full result rendering.

### 4.2 MCP Server Enhancement ✅ DONE

**File:** `internal/mcp/server.go`

JSON-RPC server implements `initialize`, `tools/list`, `tools/call`, and `resources/list`. 15 tools wired: `get_server_stats`, `list_domains`, `add_domain`, `delete_domain`, `list_accounts`, `add_account`, `delete_account`, `get_account_info`, `get_queue_status`, `retry_queue_item`, `flush_queue`, `check_dns`, `check_tls`, `get_system_status`, `reload_config`. Auth-token gating, CORS, per-IP rate limiting, and per-method tracing spans (`mcp.<method>` with `mcp.tool` attribute on `tools/call`) are included.

---

## Phase 5: Testing & Polish ✅ MOSTLY COMPLETE

### 5.1 SMTP/IMAP Integration Tests ✅ DONE

**Files:** `internal/integration/mailflow_test.go`

Covers `TestMessageDeliveryFlow`, `TestQueueProcessing`, `TestAliasResolution`, `TestDomainManagement`, `TestMessageSearchIndex`, `TestAuthenticationFlow`, `TestSMTPAuthentication`, `TestIMAPAuthentication`, `TestWebhookDelivery`, and `TestFullMailFlow`. `TestFullMailFlow` shares one `storage.MessageStore` + `storage.Database` between the SMTP delivery handler and the IMAP `BboltMailstore` via `imap.NewBboltMailstoreWithInterfaces`, mirroring the production wiring in `internal/server/server_start.go` so the SMTP→IMAP bridge is actually exercised. The IMAP test driver uses `imap.Server.SetAllowPlainAuth(true)` and SMTP uses `Config.AllowInsecure: true` to skip TLS in loopback tests; all three previously-Windows-skipped tests now run on every platform.

### 5.2 Load Tests ✅ DONE

**Files:** `load-tests/k6/{api-load,imap-load,smtp-load,websocket-load,stress-test}.js`, `load-tests/docker-compose.yml`

Five k6 scenarios cover SMTP, IMAP, REST API, WebSocket, and a stress profile. Threshold-based pass/fail with custom Trend/Rate metrics.

---

## Open Items (Beyond Phase 5)

### 6.1 JMAP Change Journal ✅ DONE

**Files:** `internal/storage/changes.go`, `internal/storage/database.go`, `internal/jmap/handlers.go`, `internal/jmap/server.go`

Added a per-user change journal in `storage.Database` with bbolt-sequence-backed state tokens, JSON entries (Seq/Type/Kind/ID/Mailbox/At), 30-day retention, and a 50K-per-user cap. `CreateMailbox`/`DeleteMailbox`/`RenameMailbox` and `StoreMessageMetadata`/`UpdateMessageMetadata`/`DeleteMessage` record entries best-effort (mutations succeed even if journal write fails). `Mailbox/changes`, `Email/changes` (newly added to dispatcher), and `Thread/changes` fold journal entries into JMAP `created/updated/destroyed` sets, with `created+destroyed` collapsing to no-op per RFC. `Identity/changes` still returns empty deltas (identities are settings-derived). All SMTP delivery, IMAP STORE/EXPUNGE, and JMAP `set` flows are covered because they all flow through `storage.Database`.

### 6.2 Re-enable `TestFullMailFlow` (and IMAP/SMTP server tests) on Windows ✅ DONE

**Files:** `internal/integration/mailflow_test.go`, `internal/imap/server.go`, `internal/imap/commands.go`

Root cause was not a test-driver I/O loop bug — IMAP `LOGIN`/`AUTHENTICATE` always returns `NO LOGIN requires TLS - use STARTTLS first` over plaintext, and the test loop only matched `EXISTS` or `A2 OK`, so it spun forever. Added `imap.Server.SetAllowPlainAuth(bool)` (default false) so loopback tests can opt out of the TLS requirement; production callers must never enable it. Set `smtp.Config.AllowInsecure: true` for the SMTP side (knob already existed). Removed the three `runtime.GOOS == "windows"` skips. All three tests now pass on Windows and verify the SMTP→IMAP bridge.

---

## Beyond v1.0: Future Enhancements

### 5.1 Horizontal Scaling
- bbolt clustering or switch to distributed store
- Redis for session/rate limit state
- Message queue for distributed processing

### 5.2 Enhanced Security
- TLS client certificates for IMAP/SMTP
- Certificate pinning
- DNSSEC validation enforcement

### 5.3 Advanced Features
- SMTP response streaming (CHUNKING)
- IMAP PROXY protocol support
- Advanced Sieve extensions (variables, relational tests)

---

## Effort Summary

| Phase | Items | Status |
|-------|-------|--------|
| Phase 1 | S/MIME fix, OpenPGP fix, Sieve ReDoS | ✅ DONE |
| Phase 2 | CalDAV, CardDAV, JMAP, tracing spans | ✅ DONE (JMAP changes-journal stubbed) |
| Phase 3 | Cache bounds, JWT bug, SPF TTL configurable, LDAP pool | ✅ DONE |
| Phase 4 | Search webmail wiring, MCP enhancement | ✅ DONE |
| Phase 5 | Integration tests, load tests | ✅ DONE |
| Phase 6 | JMAP change journal, Windows IMAP/SMTP tests | ✅ DONE |

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| (none currently tracked — see "Beyond v1.0" for forward-looking enhancements) | — | — | — |

---

## Recommended Next Steps

All planned phases are complete. Suggested next investments live under "Beyond v1.0" — horizontal scaling (Redis/cluster), TLS client certs, advanced Sieve extensions, SMTP CHUNKING.

**Status:** Production-ready for small-to-medium deployments. No remaining blockers or polish items.