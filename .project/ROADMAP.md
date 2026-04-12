# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-11
> This roadmap prioritizes work needed to bring the project to production quality.

---

## Current State Assessment

uMailServer is a **production-grade single-binary email server** for v0.1.0. Core protocols (SMTP/IMAP/POP3) are well-implemented with proper RFC compliance. Security features (SPF/DKIM/DMARC validation, JWT auth, rate limiting, brute-force protection) are solid. Build and tests pass cleanly.

**Key Blockers for Production Readiness:** ✅ ALL FIXED (2026-04-11)
1. ✅ **`signRSA` nil hash bug** — `rand.Reader` added to `rsa.SignPKCS1v15` call
2. ✅ **Spam Bayesian filter** — Robinson-Fisher algorithm fully implemented (needs training data)
3. ✅ **AV scanning** — ClamAV TCP INSTREAM protocol fully implemented (needs daemon)
4. ✅ **ARC sealing** — `Seal()` method implemented in `auth/arc.go`
5. ✅ **Architecture refactor** — Both monolith files split into focused per-subsystem files (2026-04-12)

**Key Tech Debt (Architecture — resolved 2026-04-12):**
1. ✅ `api/server.go` at 2550 lines — ✅ Split into 14 focused files (→892 lines)
2. ✅ `server/server.go` at 1689 lines — ✅ Split into 18 focused files (→284 lines)
3. Distributed tracing — OpenTelemetry initialized but no spans in code

---

## Phase 1: Critical Bug Fixes (Week 1) ✅ COMPLETED

### 1.1 `signRSA` nil hash fix (1 line)

**File:** `internal/auth/dkim.go:623`

```go
// BEFORE (bug):
signature, err := rsa.SignPKCS1v15(nil, privateKey, crypto.SHA256, hash[:])

// AFTER (fix):
signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
```

**Verification:** Add integration test that signs a message and verifies the signature against the public key.

### 1.2 `isTemporaryError` reliability fix

**File:** `internal/auth/spf.go:501-509`

Replace string-matching with proper `net.Error` type assertion:

```go
func isTemporaryError(err error) bool {
    var netErr net.Error
    if errors.As(err, &netErr) {
        return netErr.Temporary()
    }
    return false
}
```

---

## Phase 2: Core Feature Completion (Week 2-4) ✅ COMPLETED

### 2.1 Implement Bayesian Spam Filter (3-5 days)

**File:** `internal/spam/bayes.go`

The `Tokenize()` function exists but `Score()` always returns `defaultScore`. Need to:
1. Implement token frequency database (bbolt-backed)
2. Train on ham/spam (webmail actions: mark as spam/not spam)
3. Score messages using Naive Bayes formula
4. Wire into SMTP pipeline after heuristic stage

**API changes:** Need webmail endpoints to submit ham/spam feedback for training.

### 2.2 Implement ClamAV Integration (3-5 days)

**File:** `internal/av/scanner.go`

Replace stub with actual integration:
1. Connect to ClamAV daemon via TCP socket or Unix socket
2. Stream message to `INSTREAM` command
3. Parse response for virus name or "OK"
4. Add `ClamAVSocket` config option

**Config change:** `spam.av.enabled: true` + `spam.av.socket: /var/run/clamav/clamd.sock`

### 2.3 Implement ARC Sealing (2-3 days)

**File:** `internal/auth/arc.go`

`Seal()` method needs to:
1. Add `AS` header with authentication results from each hop
2. Sign the chain with the server's own key
3. Add `AF` header chain

Wire into SMTP submission pipeline for outbound relay.

### 2.4 Implement DMARC Reporting (2-3 days)

**Files:** `internal/auth/dmarc.go`, `internal/api/` (new handler)

1. Aggregate RUA reports (DMARC feedback)
2. Generate RFC 5961-compliant XML reports
3. Send via SMTP to RUA addresses in `_dmarc.example.com` TXT record
4. Per-message failure details stored for forensic reports (RUF)

### 2.5 Implement Sieve Vacation (2-3 days)

**Files:** `internal/sieve/`, `internal/vacation/`

1. Implement `vacation` action in Sieve executor
2. Track auto-replied messages to avoid duplicates
3. Wire `vacation` package into sieve execution context

---

## Phase 3: Architecture Refactoring (Week 4-6)

### 3.1 Split `api/server.go` (1-2 weeks)

Current 2536 lines. Proposed split:

```
internal/api/
  server.go          # HTTP server setup, middleware, routes
  handler/           # Per-resource handlers
    domains.go       # Domain CRUD
    accounts.go      # Account CRUD
    aliases.go      # Alias CRUD
    queue.go        # Queue management
    auth.go         # Login, TOTP setup
    config.go       # Server config
  middleware/
    auth.go         # JWT validation
    ratelimit.go    # HTTP-level rate limiting
    cors.go         # CORS
    logging.go      # Request logging
  websocket.go      # WebSocket upgrades
  sse.go           # SSE endpoint
  push.go          # Push notification hub
```

### 3.2 Split `server/server.go` (1 week)

Current 1399 lines. Proposed split:

```
internal/server/
  server.go          # Server struct, Start/Stop
  init/
    db.go           # DB initialization
    mailstore.go    # Mailstore initialization
    tls.go          # TLS manager init
    queue.go        # Queue init
    smtp.go         # SMTP servers
    imap.go         # IMAP server
    pop3.go         # POP3 server
    http.go         # HTTP API
    mcp.go          # MCP server
```

### 3.3 Fix Queue Thundering Herd (1-2 days) ✅ COMPLETED

**File:** `internal/queue/manager.go`

Bounded worker pool replaces unbounded concurrent processing:
1. Persistent workers consume from a channel with bounded concurrency
2. Semaphore limits concurrent deliveries to 20
3. Proper panic recovery with `handleDeliveryFailure` called in defer

---

## Phase 4: Stub Package Wiring (Week 5-8) ✅ COMPLETED

### 4.1 Wire Webhook System

1. Add event emission calls in:
   - `queue/manager.go` — message delivered/failed/bounced
   - `smtp/session.go` — message received, filtered
   - `api/` handlers — admin actions (domain add, account create, etc.)
2. `POST {webhook.url}` with JSON event body
3. Retry with exponential backoff
4. Dead letter queue for failed webhooks

### 4.2 Wire Push Notifications

1. Wire into `websocket/` for real-time webmail updates
2. Subscribe to new mail events in `storage/messagestore.go`
3. WebPush to subscriber endpoints stored per-account

### 4.3 Wire Alert Manager

1. Call `alert.Send()` on:
   - Queue bounce threshold exceeded
   - TLS cert expiring < 7 days
   - Disk space < 10%
   - Repeated delivery failures to same MX

### 4.4 Implement CalDAV HTTP Handlers

1. `CALDAV` HTTP endpoints at `/dav/calendars/`
2. `REPORT` method for calendar-query
3. `MKCALENDAR`, `MKCALENDAR` methods
4. iCal feed generation

### 4.5 Implement CardDAV HTTP Handlers

1. `CARDDAV` HTTP endpoints at `/dav/contacts/`
2. Address book queries
3. vCard CRUD

### 4.6 Implement JMAP Method Handlers

1. `JMAPCoreInvocation` parsing
2. `getMailboxes`, `getEmails`, `setEmails` methods
3. `Email/changes` for sync

---

## Phase 5: Hardening (Week 7-10)

### 5.1 Fuzz Testing SMTP/IMAP Parsers

1. Add `//go:build fuzz` tags for:
   - SMTP command parser
   - IMAP command parser
   - MIME parser (from `mail.ReadMessage`)
   - DKIM signature parser
   - SPF record parser
2. Run in CI fuzzing workflow

### 5.2 Security Audit for XSS in Webmail

1. Review `isomorphic-dompurify` DOMPurify config in webmail
2. Test XSS vectors in email rendering
3. Verify CSP headers in API responses

### 5.3 Circuit Breaker in MX Delivery

Wire `internal/circuitbreaker/` into `queue/manager.go`:
1. Per-MX-host circuit breakers
2. Open on consecutive failures
3. Half-open after timeout, allow one probe
4. Close on success

### 5.4 Account Portal `make build-web` Integration

Add `web/account/` build to `build-web` target in Makefile.

---

## Phase 6: Missing Spec Features (Week 10+)

### 6.1 S/MIME Encryption

1. Parse S/MIME signed/encrypted messages (PKCS#7)
2. Store/decrypt for IMAP display
3. Sign outgoing messages with user's key

### 6.2 OpenPGP Support

1. Parse PGP-armored messages
2. Decrypt with private key
3. Encrypt/sign outgoing with recipient's public key

### 6.3 LDAP Full Integration

1. Test `internal/auth/ldap.go` with real LDAP server
2. Group-based ACLs
3. LDAP address book for autocomplete

---

## Priority Summary

| Priority | Task | Effort | Impact |
|----------|------|--------|--------|
| P0 | Fix `signRSA` nil hash bug | 1 line | Correctness |
| P0 | Fix `isTemporaryError` | 10 lines | Correctness |
| P1 | Implement Bayesian filter | 3-5 days | Core feature |
| P1 | Implement ClamAV integration | 3-5 days | Core feature |
| P1 | Implement ARC sealing | 2-3 days | RFC compliance |
| P1 | Fix queue thundering herd | 1-2 days | Production stability |
| P2 | Implement DMARC reporting | 2-3 days | Spec compliance |
| P2 | Wire webhook system | 3-5 days | Integration |
| P2 | Implement Sieve vacation | 2-3 days | Feature parity |
| P3 | Split api/server.go | 1-2 weeks | ✅ DONE (2026-04-12) |
| P3 | Split server/server.go | 1 week | ✅ DONE (2026-04-12) |
| P3 | Wire push/alert systems | 3-5 days | ✅ DONE |
| P4 | CalDAV/CardDAV handlers | 1-2 weeks | ✅ DONE |
| P4 | JMAP method handlers | 1-2 weeks | Feature parity |
| P5 | S/MIME encryption | 2-3 weeks | Spec compliance |
| P5 | OpenPGP support | 2-3 weeks | Spec compliance |

**Estimated total for production quality (P0-P3):** 6-8 weeks
