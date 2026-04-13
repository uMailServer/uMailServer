# uMailServer Security Report

**Scan Date:** 2026-04-13  
**Scope:** Full Go backend codebase (`internal/`, `cmd/`, `embed.go`, `go.mod`)  
**Methodology:** Multi-phase AI-assisted security audit — Recon, Hunt, Verify

---

## Executive Summary

| Category | Critical | High | Medium | Low | Info |
|----------|----------|------|--------|-----|------|
| Authentication & Access Control | 2 | 6 | 7 | 1 | 0 |
| Injection & RCE | 0 | 1 | 0 | 1 | 0 |
| Data Exposure & Crypto | 0 | 0 | 2 | 7 | 1 |
| Concurrency & Go-Specific | 2 | 10 | 13 | 6 | 0 |
| **Total** | **4** | **17** | **22** | **15** | **1** |

### Top 10 Priorities (Fix First)

1. **CRITICAL** — Vacation reply double-lock deadlock (`internal/server/server_vacation.go`)
2. **CRITICAL** — `/api/v1/auth/refresh` bypasses `authMiddleware` entirely (`internal/api/server_auth.go`)
3. **CRITICAL** — SSE endpoint `/api/v1/events` has no authentication (`internal/api/server.go`)
4. **HIGH** — SMTP header injection (CRLF) in mail send API (`internal/api/mail.go`)
5. **HIGH** — Unbounded goroutine creation on every message delivery (`internal/server/server_handlers.go`)
6. **HIGH** — IMAP MDN goroutine has no panic recovery (`internal/imap/mailstore.go`)
7. **HIGH** — Quota update race condition / TOCTOU (`internal/server/server_handlers.go`)
8. **HIGH** — Any authenticated user can list all accounts (`internal/api/server_accounts.go`)
9. **HIGH** — Token blacklist is in-memory only, lost on restart (`internal/api/server_auth.go`)
10. **HIGH** — `ratelimit.cleanupLoop` goroutine leak, no `Stop()` method (`internal/ratelimit/ratelimit.go`)

---

## 1. Authentication & Access Control

### 1.1 Missing auth on `/api/v1/auth/refresh` (Critical)
- **File:** `internal/api/server.go:368`, `internal/api/server_auth.go:262-299`
- **Issue:** The refresh route is registered directly on `mux` instead of the `api` sub-router, so it bypasses `authMiddleware`. The handler reads `r.Context().Value("user")` (nil) and generates a JWT with `"sub": nil`.
- **Fix:** Move the route under the `api` mux (which has auth middleware) or wrap it explicitly with `authMiddleware`.

### 1.2 SSE endpoint `/api/v1/events` has no auth (Critical)
- **File:** `internal/api/server.go:361`
- **Issue:** The SSE endpoint is mounted on the main `mux` without auth middleware. It relies on `SetAuthFunc` being called, but if that returns permissive defaults, it is unauthenticated.
- **Fix:** Enforce auth middleware on the SSE endpoint.

### 1.3 JWT `alg` none not explicitly rejected (High)
- **File:** `internal/api/server.go:643-659`, `internal/api/admin.go:128-144`
- **Issue:** The code relies on `token.Method.(*jwt.SigningMethodHMAC)` type assertion rather than explicitly allowlisting `HS256`. The `kid` header is used without existence/type validation.
- **Fix:** Explicitly check `token.Header["alg"] == "HS256"` and validate `kid` is a string.

### 1.4 `listAccounts` allows horizontal privilege escalation (High)
- **File:** `internal/api/server_accounts.go:77`
- **Issue:** Any authenticated non-admin user can call `GET /api/v1/accounts` and list all accounts across all domains.
- **Fix:** Restrict `listAccounts` to admins. Non-admins should only see their own account.

### 1.5 `updateAccount` allows privilege escalation via empty auth context (High)
- **File:** `internal/api/server_accounts.go:204-276`
- **Issue:** The check `if authUser != "" && !isAdmin && req.IsAdmin` can be bypassed if `authUser` is empty.
- **Fix:** Remove the `authUser != ""` guard; always enforce admin checks.

### 1.6 Weak password policy (Medium)
- **File:** `internal/api/validators.go:68-76`
- **Issue:** Minimum 8 characters, no complexity requirements.
- **Fix:** Add uppercase, lowercase, digit, and special character requirements.

### 1.7 Rate-limit config PUT lacks bounds validation (Medium)
- **File:** `internal/api/ratelimit.go:67-126`
- **Issue:** No upper bounds on rate-limit fields; negative values allowed for some fields.
- **Fix:** Add validation: all fields non-negative and capped (e.g., max 100,000).

### 1.8 Login rate limit is IP-only and bypassable (Medium)
- **File:** `internal/api/server_auth.go:64-87`
- **Issue:** 5 attempts per IP per 5 minutes. Distributed attacks or IPv6 rotation bypass it easily.
- **Fix:** Add per-account rate limiting and exponential backoff.

### 1.9 API rate limit skipped for auth endpoints (Medium)
- **File:** `internal/api/server.go:470-495`
- **Issue:** `rateLimitMiddleware` skips `/api/v1/auth/*`. Refresh and logout have no rate limiting.
- **Fix:** Remove blanket auth exemption; apply limits with higher thresholds if needed.

### 1.10 SMTP inbound allows AUTH over plaintext if `AllowInsecure` is set (High)
- **File:** `internal/smtp/session.go:708-746`
- **Issue:** Port 25 SMTP server does not hardcode `RequireTLS: true` for AUTH.
- **Fix:** Never allow `AllowInsecure` on port 25. Only allow it on submission with a warning.

### 1.11 POP3 USER/PASS allowed over plaintext (High)
- **File:** `internal/pop3/server.go:473-650`
- **Issue:** No TLS requirement before `USER`/`PASS` authentication.
- **Fix:** Add `RequireTLS` config and reject auth if TLS is not active.

### 1.12 APOP uses MD5 and is vulnerable to offline cracking (Medium)
- **File:** `internal/pop3/server.go:464-470`, `internal/pop3/server.go:537-589`
- **Issue:** APOP digest uses `MD5(timestamp + secret)`. MD5 is broken. Static SHA256(password) stored as APOP secret is brute-forceable offline.
- **Fix:** Deprecate/disable APOP by default. Use SASL PLAIN or SCRAM-SHA-256 over TLS.

### 1.13 TOTP not rate-limited (Medium)
- **File:** `internal/api/server_auth.go:178-190`
- **Issue:** No per-account TOTP attempt limiting beyond IP-based login rate limit.
- **Fix:** Add per-account TOTP attempt limiting (e.g., 5 failures = cooldown).

### 1.14 TOTP secret stored in plaintext (High)
- **File:** `internal/api/server_totp.go:43-55`, `internal/db/db.go`
- **Issue:** TOTP secrets are stored in bbolt as base32 plaintext.
- **Fix:** Encrypt TOTP secrets with AES-256-GCM using a server master key.

### 1.15 TOTP replay within time window (Low)
- **File:** `internal/auth/totp.go:69-98`
- **Issue:** No tracking of used TOTP codes; same code reusable within the 90-second drift window.
- **Fix:** Store the last used TOTP time step per account and reject reuse.

### 1.16 JWT secret rotation never prunes old secrets (Medium)
- **File:** `internal/api/server_admin.go:13-35`
- **Issue:** `handleJWTRotate` appends new secrets but never removes old ones. Compromised old secrets remain valid forever.
- **Fix:** Implement a pruning policy (keep last N secrets or age-based eviction).

### 1.17 Token blacklist is in-memory only (High)
- **File:** `internal/api/server_auth.go:30-62`
- **Issue:** Revoked tokens are stored in a `map[string]time.Time`. Lost on restart or in multi-instance deployments.
- **Fix:** Persist revoked tokens to bbolt with TTL cleanup, or use proper refresh-token rotation in the database.

### 1.18 Refresh token endpoint does not validate old token before revocation (High)
- **File:** `internal/api/server_auth.go:262-299`
- **Issue:** No refresh token rotation tracking. A stolen token can be refreshed indefinitely.
- **Fix:** Protect with `authMiddleware` and implement single-use refresh tokens stored in the database.

### 1.19 LDAP anonymous bind possible when `BindDN` is empty (High)
- **File:** `internal/auth/ldap.go:174-194`
- **Issue:** If `BindDN == ""`, the service account bind is skipped, potentially allowing anonymous binds.
- **Fix:** Require `BindDN` when LDAP is enabled, or explicitly reject empty `BindDN`.

### 1.20 LDAP credentials exposed in config (Medium)
- **File:** `internal/auth/ldap.go:16-30`
- **Issue:** `BindPassword` can be accidentally logged or serialized.
- **Fix:** Add `json:"-"` tag and custom marshaling to prevent serialization.

### 1.21 LDAP user filter injection risk via configuration (Medium)
- **File:** `internal/auth/ldap.go:197`, `internal/auth/ldap.go:302`
- **Issue:** `UserFilter` uses `fmt.Sprintf` with `ldap.EscapeFilter(username)`. If an attacker compromises `UserFilter` via config, they can inject arbitrary filters.
- **Fix:** Validate `UserFilter` at config load time (exactly one `%s`, balanced parentheses).

---

## 2. Injection & RCE

### 2.1 SMTP Header Injection (CRLF) in `handleMailSend` (High)
- **File:** `internal/api/mail.go:395-406`
- **Issue:** `req.Subject`, `req.To`, `req.CC`, and `req.BCC` are directly interpolated into raw email headers without CRLF sanitization.
- **Impact:** An attacker can inject arbitrary headers or a new message body via `\r\n`.
- **Fix:** Sanitize or reject CRLF sequences; validate email addresses; consider using a proper MIME library.

### 2.2 CORS wildcard origin risk (Low)
- **File:** `internal/api/server.go:590-616`
- **Issue:** `CorsOrigins` can be configured to `"*"`, returning `Access-Control-Allow-Origin: *`.
- **Fix:** Document that `*` should not be used in production with credentials.

### Clean Categories
- **SQL/NoSQL Injection:** None (uses bbolt key-value store)
- **LDAP Injection:** None (proper `ldap.EscapeFilter` usage)
- **Command Injection:** None (no `os/exec` in production code)
- **SSRF:** None (webhook/alert managers block private IPs)
- **XXE:** None (Go's `encoding/xml` does not resolve external entities)
- **XSS/SSTI (backend):** None (no template engine in backend)
- **RCE:** None (no unsafe deserialization or dynamic code evaluation)

---

## 3. Data Exposure & Cryptography

### 3.1 APOP authentication uses broken MD5 hash (Medium)
- **File:** `internal/pop3/server.go:465-470`, `internal/pop3/server.go:560-570`
- **Issue:** APOP uses `crypto/md5` which is cryptographically broken.
- **Fix:** Disable APOP by default; deprecate in documentation.

### 3.2 Predictable POP3 session ID generation (Medium)
- **File:** `internal/pop3/server.go:893-896`
- **Issue:** `generateSessionID()` uses `time.Now().UnixNano()` as the only entropy source.
- **Fix:** Use `crypto/rand`:
  ```go
  b := make([]byte, 16); rand.Read(b); return hex.EncodeToString(b)
  ```

### 3.3 CRAM-MD5 uses HMAC-MD5 (Low)
- **File:** `internal/auth/cram.go`
- **Issue:** Legacy deprecated mechanism.
- **Fix:** Prefer SCRAM-SHA-256 or PLAIN over TLS. Disable CRAM-MD5 by default.

### 3.4 LDAP TLS allows `InsecureSkipVerify` (Low)
- **File:** `internal/auth/ldap.go:148-152`, `internal/auth/ldap.go:157-161`
- **Issue:** `SkipVerify` config disables certificate validation.
- **Fix:** Log a prominent warning when enabled; document as development-only.

### 3.5 Validation errors exposed directly to API clients (Low)
- **Files:** `internal/api/server_accounts.go:133-141`, `internal/api/server_domains.go:92-95`
- **Issue:** Internal validator error strings sent directly to HTTP clients.
- **Fix:** Map to generic client-facing messages; log details server-side.

### 3.6 Log file created with overly permissive mode (Low)
- **File:** `internal/logging/rotate.go:66`
- **Issue:** `os.OpenFile(..., 0644)` makes logs world-readable.
- **Fix:** Use `0600`.

### 3.7 CardDAV metadata updated with permissive mode (Low)
- **File:** `internal/carddav/storage.go:199`, `internal/carddav/storage.go:267`
- **Issue:** `os.WriteFile(..., 0644)` on metadata updates.
- **Fix:** Use `0600`.

### 3.8 CalDAV metadata updated with permissive mode (Low)
- **File:** `internal/caldav/storage.go:199`, `internal/caldav/storage.go:267`
- **Issue:** Same as 3.7.
- **Fix:** Use `0600`.

### 3.9 bbolt databases lack encryption at rest (Low)
- **Files:** `internal/db/db.go:138`, `internal/storage/database.go:33`
- **Issue:** Password hashes, TOTP secrets, and DKIM keys stored in plaintext on disk.
- **Fix:** Recommend full-disk encryption for hardened deployments. Encrypt sensitive fields at application level if needed.

### 3.10 Outdated APOP Hash comment (Info)
- **File:** `internal/db/db.go:41`
- **Issue:** Comment says `// MD5(password)` but actual code stores SHA-256.
- **Fix:** Update comment to `// SHA-256(password)`.

---

## 4. Concurrency & Go-Specific Issues

### 4.1 Double-lock deadlock in vacation reply cleanup (Critical)
- **File:** `internal/server/server_vacation.go`
- **Issue:** `sendVacationReply` locks `vacationRepliesMu`, then calls `cleanupVacationReplies()` which tries to lock the same mutex again. Guaranteed deadlock once map exceeds 100 entries.
- **Fix:** Refactor to `cleanupVacationRepliesLocked()` that assumes the lock is already held.

### 4.2 Quota update race condition (High)
- **File:** `internal/server/server_handlers.go:200-220`
- **Issue:** `account.QuotaUsed` is read, modified, and written back non-atomically. Concurrent deliveries to the same account cause lost updates.
- **Fix:** Perform read-modify-write inside a single bbolt transaction or add a dedicated atomic quota update method.

### 4.3 TOCTOU race in `MessageStore.StoreMessage` (High)
- **File:** `internal/storage/messagestore.go:94-99`
- **Issue:** `os.Stat(msgPath)` checks for existence, then `os.WriteFile` writes. Race between check and use.
- **Fix:** Use `os.O_CREATE|os.O_EXCL|os.O_WRONLY` for atomic create-or-fail semantics.

### 4.4 `countMessageRefs` violates caller-holds-lock contract (High)
- **File:** `internal/queue/manager.go:400-410`
- **Issue:** Comment says "caller must hold mu", but the function acquires its own `RLock()`. If the caller holds a write lock, this deadlocks.
- **Fix:** Remove the `RLock`/`RUnlock` from `countMessageRefs`; rename to `countMessageRefsLocked()`.

### 4.5 Unbounded goroutine creation in `deliverLocal` (High)
- **File:** `internal/server/server_handlers.go:230-260`
- **Issue:** Every local delivery spawns two goroutines (push notification + vacation reply). Under high mail volume, this exhausts memory and scheduler capacity.
- **Fix:** Use a bounded worker pool or semaphore to limit concurrent background tasks.

### 4.6 `ratelimit.cleanupLoop` goroutine leak (High)
- **File:** `internal/ratelimit/ratelimit.go:80-100`
- **Issue:** `cleanupLoop` is started in `NewRateLimiter` but there is no `Stop()` method. Leaks one goroutine per instance.
- **Fix:** Add a `Stop()` method with `stopCh` and `sync.Once`.

### 4.7 Unhandled panic in IMAP MDN goroutine (High)
- **File:** `internal/imap/mailstore.go:600-620`
- **Issue:** MDN handler goroutine has no `recover()`. A panic crashes the entire server.
- **Fix:** Add `defer recover()` wrapper inside the goroutine.

### 4.8 Unhandled panic in push notification goroutine (High)
- **File:** `internal/server/server_handlers.go:235`
- **Issue:** No `recover()` in the push goroutine.
- **Fix:** Add `defer recover()` or route through a supervised worker pool.

### 4.9 Unhandled panic in vacation reply goroutine (High)
- **File:** `internal/server/server_handlers.go:245`
- **Issue:** No `recover()` in the vacation goroutine.
- **Fix:** Same as 4.8.

### 4.10 Notification hub timer pressure (Medium)
- **File:** `internal/imap/notifications.go:40-60`
- **Issue:** `Notify` creates a `time.After(100ms)` per subscriber per notification, causing timer churn.
- **Fix:** Use `select { case ch <- notification: default: }` or reuse a `time.Timer`.

### 4.11 `Manager.mu` held across I/O in `Enqueue` (Medium)
- **File:** `internal/queue/manager.go:80-120`
- **Issue:** `m.mu.Lock()` is held during disk write (`os.WriteFile`) and database updates, serializing all enqueue operations.
- **Fix:** Minimize critical section: write to disk and generate ID outside the lock, then only lock for map/db metadata updates.

### 4.12 `acquireMXConn` holds pool lock during network reset (Medium)
- **File:** `internal/queue/manager.go:500-520`
- **Issue:** `pool.mu.Lock()` is held while calling `conn.client.Reset()`. A hung SMTP server blocks the entire MX pool.
- **Fix:** Release the pool lock before calling `Reset()`, or remove the connection from the pool first.

### 4.13 IMAP MDN goroutine without lifecycle tracking (Medium)
- **File:** `internal/imap/mailstore.go:600-620`
- **Issue:** Unlimited MDN goroutines can run concurrently.
- **Fix:** Add a semaphore to bound concurrent MDN sends.

### 4.14 HTTP servers lack explicit shutdown (Medium)
- **Files:** `internal/server/server_caldav.go`, `internal/server/server_carddav.go`, `internal/server/server_jmap.go`
- **Issue:** CalDAV, CardDAV, and JMAP servers use `srv.ListenAndServe()` but `server_stop.go` only cancels context. `http.Server` requires `Shutdown()` to stop accepting connections.
- **Fix:** Store `*http.Server` in the `Server` struct and call `srv.Shutdown(ctx)` during stop.

### 4.15 Queue manager ignores context cancellation (Medium)
- **File:** `internal/queue/manager.go`
- **Issue:** Long-running SMTP deliveries do not consistently check `ctx.Done()`.
- **Fix:** Pass `context.Context` into delivery functions and use `net.Dialer` with context support.

### 4.16 Unclosed bbolt databases in error paths (High)
- **Files:** `internal/db/db.go`, `internal/storage/database.go`, `internal/imap/mailstore.go`
- **Issue:** `bolt.Open` is called in initialization paths, but if a subsequent step fails, `db.Close()` is not always called before returning.
- **Fix:** Audit all `bolt.Open` call sites and use deferred close on error paths.

### 4.17 MX connection pool leak on panic (Medium)
- **File:** `internal/queue/manager.go`
- **Issue:** If a panic occurs while an MX connection is checked out, `releaseMXConn` may never be called.
- **Fix:** Use `defer` to guarantee release, or wrap connection usage in a helper:
  ```go
  func (m *Manager) withMXConn(host string, fn func(*mxConn) error) error { ... }
  ```

### 4.18 Temporary files in Maildir may leak (Medium)
- **File:** `internal/store/maildir.go`
- **Issue:** If an error occurs during flag updates before rename/close, temp file descriptors may leak.
- **Fix:** Ensure all `os.Create`/`os.OpenFile` calls have matching `Close()` via `defer`, and clean up temp files on error with `os.Remove(tmpPath)`.

### 4.19 `QuotaUsed` integer overflow without check (High)
- **File:** `internal/server/server_handlers.go:210`
- **Issue:** `account.QuotaUsed += int64(len(data))` can overflow `int64`, wrapping negative and bypassing quota enforcement.
- **Fix:** Add overflow-checked arithmetic:
  ```go
  if account.QuotaUsed > math.MaxInt64 - int64(len(data)) { return fmt.Errorf("quota overflow") }
  account.QuotaUsed += int64(len(data))
  ```

### 4.20 Message size limits not enforced before hash/storage (Medium)
- **File:** `internal/storage/messagestore.go:72`
- **Issue:** `StoreMessage` accepts arbitrary `[]byte` with no maximum size check. Enormous messages can exhaust memory during SHA-256 hashing.
- **Fix:** Enforce a max message size at the SMTP ingestion layer and add a defensive check in `StoreMessage`.

### 4.21 Weak hash in config watcher (Low)
- **File:** `internal/config/watcher.go:154-167`
- **Issue:** `fileHash` sums bytes and casts to `string(rune(sum))`. Massive collision potential.
- **Fix:** Use `sha256.Sum256` or `crc32.ChecksumIEEE`.

---

## Appendix A: Attack Surface Summary

**Network-Facing Protocols**
- SMTP (25, 587, 465) — `internal/smtp/`
- IMAP (143, 993) — `internal/imap/`
- POP3 (995) — `internal/pop3/`
- HTTP API (443/HTTPS, 8443 admin) — `internal/api/`
- ManageSieve (4190/4191) — `internal/sieve/`
- MCP (3000) — `internal/mcp/`
- CalDAV / CardDAV / JMAP — `internal/caldav/`, `internal/carddav/`, `internal/jmap/`

**No `os/exec` in production code.** All external interaction is via TCP (ClamAV, LDAP, ACME, SMTP relay).  
**Go `encoding/xml` is safe against XXE by default.**  
**bbolt key-value store eliminates SQL injection.**

---

## Appendix B: Dependency Audit (go.mod)

| Dependency | Purpose | Notes |
|------------|---------|-------|
| `go.etcd.io/bbolt` | Embedded KV | No known critical CVEs at scan time |
| `github.com/golang-jwt/jwt/v5` | JWT | Keep updated; alg confusion is a common pitfall |
| `golang.org/x/crypto` | bcrypt, argon2, autocert | Keep updated |
| `github.com/miekg/dns` | DNS ops | Used for SPF/DKIM/DMARC/RBL |
| `github.com/go-ldap/ldap/v3` | LDAP auth | Proper escape-filter usage observed |
| `github.com/emersion/go-imap` | IMAP client | Used in tests/migrations only |

No supply-chain red flags identified.

---

*Report generated by Claude Code security-check skill.*
