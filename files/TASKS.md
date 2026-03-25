# uMailServer — Task Breakdown

## Overview

Total estimated implementation: ~14 weeks for v1.0 full stack
Phases run partially in parallel where possible.

---

## Phase 1: Foundation (Weeks 1-2)

### 1.1 Project Skeleton
- [ ] Initialize Go module: `github.com/umailserver/umailserver`
- [ ] Create directory structure per IMPLEMENTATION.md
- [ ] Set up `cmd/umailserver/main.go` with subcommand dispatch
- [ ] Implement `version` command with ldflags injection
- [ ] Create `Makefile` with build, test, release, docker targets
- [ ] Create `.goreleaser.yml` for automated releases
- [ ] Create `Dockerfile` (multi-stage: node + go + alpine)
- [ ] Create `.github/workflows/ci.yml` (lint, test, build)
- [ ] Create `umailserver.yaml.example` with all options documented
- [ ] Write initial `README.md`

### 1.2 Configuration System
- [ ] Implement `internal/config/config.go` — full Config struct
- [ ] Implement `internal/config/defaults.go` — sensible defaults
- [ ] Implement YAML loading with `gopkg.in/yaml.v3`
- [ ] Implement environment variable overrides (`UMAILSERVER_*` prefix)
- [ ] Implement config validation (required fields, valid ranges)
- [ ] Implement `internal/config/size.go` — parse "5GB", "50MB" strings
- [ ] Unit tests for config loading, env overrides, validation

### 1.3 Storage Layer
- [ ] Implement `internal/store/maildir.go` — Maildir++ full implementation
  - [ ] `Deliver()` — atomic tmp → new delivery
  - [ ] `Fetch()` — read message by filename
  - [ ] `Move()` — rename between folders
  - [ ] `SetFlags()` — update :2,FLAGS suffix
  - [ ] `List()` — enumerate folder contents
  - [ ] `Delete()` — remove message
  - [ ] `CreateFolder()` / `DeleteFolder()` / `RenameFolder()`
  - [ ] `Quota()` — calculate usage
- [ ] Implement Maildir filename generation (unique, RFC-compliant)
- [ ] Implement `internal/store/metadata.go` — bbolt-backed message metadata
- [ ] Unit tests: delivery, flag update, folder operations, quota

### 1.4 Embedded Database
- [ ] Implement `internal/db/db.go` — bbolt wrapper with bucket helpers
- [ ] Define bucket schema (accounts, domains, queue, sessions, etc.)
- [ ] Implement generic CRUD helpers for each bucket
- [ ] Implement `internal/db/accounts.go` — account CRUD
- [ ] Implement `internal/db/domains.go` — domain CRUD
- [ ] Unit tests for all DB operations

### 1.5 Logging & Observability Foundation
- [ ] Set up `log/slog` structured logging throughout
- [ ] Implement log level configuration (debug, info, warn, error)
- [ ] Implement `internal/metrics/prometheus.go` — basic counters/gauges
- [ ] Implement `/health` and `/metrics` HTTP endpoints

---

## Phase 2: SMTP Server (Weeks 2-5)

### 2.1 SMTP Core
- [ ] Implement `internal/smtp/server.go` — TCP listener with TLS support
- [ ] Implement `internal/smtp/session.go` — SMTP state machine
  - [ ] EHLO/HELO with capability advertisement
  - [ ] STARTTLS upgrade
  - [ ] MAIL FROM parsing (address + SIZE parameter)
  - [ ] RCPT TO parsing (max recipients check)
  - [ ] DATA reception with dot-stuffing
  - [ ] RSET, NOOP, QUIT
  - [ ] PIPELINING support
  - [ ] 8BITMIME support
  - [ ] SMTPUTF8 support
  - [ ] CHUNKING/BDAT support
- [ ] Implement connection timeouts (read: 5min, write: 5min, idle: 5min)
- [ ] Implement max connection limit per IP
- [ ] Unit tests for each SMTP command and state transition
- [ ] Integration test: send a full message via SMTP, verify in Maildir

### 2.2 SMTP Authentication
- [ ] Implement AUTH PLAIN (base64 decode → verify)
- [ ] Implement AUTH LOGIN (challenge-response)
- [ ] Implement AUTH CRAM-MD5 (HMAC challenge)
- [ ] Require TLS before AUTH
- [ ] Implement `internal/auth/user.go` — password verification (Argon2id)
- [ ] Unit tests for each AUTH mechanism

### 2.3 Submission Server
- [ ] Port 587 (STARTTLS) listener
- [ ] Port 465 (implicit TLS) listener
- [ ] Require authentication for submission
- [ ] DKIM sign outbound messages before queuing
- [ ] Add Message-ID if missing
- [ ] Add Date header if missing
- [ ] Unit tests for submission flow

### 2.4 Message Pipeline
- [ ] Implement `internal/smtp/pipeline.go` — stage-based pipeline
- [ ] Implement rate limit stage
- [ ] Implement SPF stage
- [ ] Implement DKIM verify stage
- [ ] Implement DMARC stage
- [ ] Implement greylisting stage
- [ ] Implement RBL stage
- [ ] Implement heuristic stage
- [ ] Implement Bayesian stage
- [ ] Implement score aggregator stage
- [ ] Implement delivery stage (local mailbox or outbound queue)
- [ ] Integration test: full pipeline with spam scoring

### 2.5 Outbound Queue
- [ ] Implement `internal/queue/manager.go` — persistent queue in bbolt
- [ ] Implement `internal/queue/scheduler.go` — retry scheduler goroutine
- [ ] Implement MX lookup for recipient domains
- [ ] Implement outbound SMTP client (connect to remote MX)
- [ ] Implement TLS negotiation for outbound (STARTTLS, MTA-STS check)
- [ ] Implement exponential backoff retry (5m → 48h → bounce)
- [ ] Implement `internal/queue/bounce.go` — DSN generation
- [ ] Implement queue CLI commands (list, retry, flush, drop)
- [ ] Unit tests for queue operations, retry logic, bounce generation

---

## Phase 3: Authentication Protocols (Weeks 3-6)

### 3.1 SPF
- [ ] Implement `internal/auth/spf.go` — full RFC 7208
  - [ ] TXT record lookup and parsing
  - [ ] Mechanism evaluation: ip4, ip6, a, mx, include, exists, redirect
  - [ ] Qualifier handling: +, -, ~, ?
  - [ ] DNS lookup limit (max 10)
  - [ ] Void lookup limit (max 2)
- [ ] Unit tests with RFC 7208 test vectors

### 3.2 DKIM
- [ ] Implement `internal/auth/dkim.go` — signing
  - [ ] RSA-SHA256 signing
  - [ ] Ed25519-SHA256 signing
  - [ ] Relaxed/relaxed canonicalization
  - [ ] Header selection (From, To, Subject, Date, Message-ID, etc.)
  - [ ] Key generation (2048-bit RSA, Ed25519)
  - [ ] DNS record generation (for admin UI)
- [ ] Implement DKIM verification
  - [ ] Signature parsing
  - [ ] Public key lookup via DNS
  - [ ] Body hash verification
  - [ ] Header hash verification
- [ ] Implement key rotation (generate new key, publish DNS, retire old)
- [ ] Unit tests with RFC 6376 test vectors

### 3.3 DMARC
- [ ] Implement `internal/auth/dmarc.go` — full RFC 7489
  - [ ] Policy record lookup (_dmarc.domain)
  - [ ] Policy parsing (p=, sp=, rua=, ruf=, pct=, adkim=, aspf=)
  - [ ] SPF alignment check (strict and relaxed)
  - [ ] DKIM alignment check (strict and relaxed)
  - [ ] Policy application based on alignment results
  - [ ] Percentage sampling (pct=)
- [ ] Unit tests for all DMARC scenarios

### 3.4 ARC
- [ ] Implement `internal/auth/arc.go` — ARC chain verification
  - [ ] ARC-Authentication-Results parsing
  - [ ] ARC-Message-Signature verification
  - [ ] ARC-Seal verification
  - [ ] Chain validation (i= sequencing)
- [ ] Implement ARC sealing for forwarded messages
- [ ] Unit tests

### 3.5 MTA-STS
- [ ] Implement `internal/auth/mtasts.go`
  - [ ] _mta-sts.domain TXT record lookup
  - [ ] Policy file fetching (HTTPS)
  - [ ] Policy parsing (mode, mx patterns, max_age)
  - [ ] Policy caching
  - [ ] Enforcement on outbound delivery
- [ ] Unit tests

### 3.6 DANE
- [ ] Implement `internal/auth/dane.go`
  - [ ] TLSA record lookup
  - [ ] Certificate matching (usage 2, 3)
  - [ ] Integration with outbound TLS verification
- [ ] Unit tests

---

## Phase 4: IMAP Server (Weeks 4-8)

### 4.1 IMAP Core
- [ ] Implement `internal/imap/server.go` — TCP listener (TLS + STARTTLS)
- [ ] Implement `internal/imap/session.go` — state machine
- [ ] Implement `internal/imap/parser.go` — IMAP command parser
  - [ ] Sequence set parsing (1:*, 1,3,5, 1:10)
  - [ ] FETCH item parsing
  - [ ] SEARCH criteria parsing
  - [ ] Literal parsing ({N} and {N+})
  - [ ] Quoted string / atom parsing

### 4.2 IMAP Commands — Not Authenticated
- [ ] CAPABILITY (advertise extensions)
- [ ] LOGIN
- [ ] AUTHENTICATE PLAIN
- [ ] STARTTLS
- [ ] LOGOUT

### 4.3 IMAP Commands — Authenticated
- [ ] SELECT / EXAMINE (open mailbox, return status)
- [ ] CREATE (create folder)
- [ ] DELETE (delete folder)
- [ ] RENAME (rename folder)
- [ ] SUBSCRIBE / UNSUBSCRIBE
- [ ] LIST (list folders with pattern matching)
- [ ] STATUS (folder stats without selecting)
- [ ] APPEND (upload message to folder)
- [ ] NAMESPACE

### 4.4 IMAP Commands — Selected
- [ ] CHECK
- [ ] CLOSE
- [ ] EXPUNGE (permanently remove deleted messages)
- [ ] SEARCH (criteria-based message search)
- [ ] FETCH (retrieve message data)
  - [ ] FLAGS, INTERNALDATE, RFC822.SIZE
  - [ ] ENVELOPE (parsed from headers)
  - [ ] BODYSTRUCTURE / BODY (MIME structure)
  - [ ] BODY[section] (partial message retrieval)
  - [ ] BODY.PEEK[section] (without setting \Seen)
- [ ] STORE (update flags)
- [ ] COPY (copy messages between folders)
- [ ] MOVE (RFC 6851)
- [ ] UID variants of SEARCH, FETCH, STORE, COPY, MOVE

### 4.5 IMAP Extensions
- [ ] IDLE (RFC 2177) — real-time push notifications
- [ ] CONDSTORE (RFC 7162) — conditional store with MODSEQ
- [ ] SPECIAL-USE (RFC 6154) — well-known folder attributes
- [ ] LITERAL+ (RFC 7888) — non-synchronizing literals
- [ ] ID (RFC 2971) — client identification
- [ ] SORT (RFC 5256) — server-side sorting
- [ ] THREAD (RFC 5256) — conversation threading
- [ ] COMPRESS=DEFLATE (RFC 4978) — connection compression

### 4.6 IMAP Integration
- [ ] Integration test: full IMAP session (login → select → fetch → store → logout)
- [ ] Test with Thunderbird (manual)
- [ ] Test with Apple Mail (manual)
- [ ] Test with Outlook (manual)
- [ ] Test with K-9 Mail / Android (manual)

---

## Phase 5: Spam Engine (Weeks 5-8)

### 5.1 Bayesian Classifier
- [ ] Implement `internal/spam/bayesian.go`
  - [ ] Tokenizer (unigrams + bigrams + meta-tokens)
  - [ ] Training function (update token frequencies)
  - [ ] Classification (Robinson-Fisher method)
  - [ ] Per-user training data in bbolt
- [ ] Unit tests with sample ham/spam corpus

### 5.2 RBL/DNSBL
- [ ] Implement `internal/spam/rbl.go`
  - [ ] IP reversal
  - [ ] Parallel DNS lookups
  - [ ] Result code interpretation
  - [ ] Configurable server list
  - [ ] Caching (TTL-based)
- [ ] Unit tests (mock DNS)

### 5.3 Heuristic Rules
- [ ] Implement `internal/spam/heuristic.go`
  - [ ] 15+ default rules (see IMPLEMENTATION.md)
  - [ ] Per-rule scoring
  - [ ] Rule enable/disable per domain
- [ ] Unit tests for each rule

### 5.4 Greylisting
- [ ] Implement `internal/spam/greylist.go`
  - [ ] Triplet tracking (sender IP + sender email + recipient)
  - [ ] Configurable delay (default 5 min)
  - [ ] Whitelist after first retry (auto-learn)
  - [ ] Expiry for old triplets
- [ ] Unit tests

### 5.5 Score Aggregation
- [ ] Implement `internal/spam/scorer.go`
  - [ ] Combine all layer scores
  - [ ] Apply configurable thresholds
  - [ ] Return final verdict (inbox / junk / quarantine / reject)
- [ ] Add X-Spam-Score and X-Spam-Status headers

---

## Phase 6: TLS & Certificates (Weeks 3-5)

### 6.1 ACME Client
- [ ] Implement `internal/tls/acme.go`
  - [ ] Account creation with Let's Encrypt
  - [ ] HTTP-01 challenge handler
  - [ ] DNS-01 challenge support (Cloudflare API)
  - [ ] Certificate issuance flow
  - [ ] Certificate storage on disk
  - [ ] Auto-renewal goroutine (check every 12 hours)
- [ ] Implement SNI-based certificate selection
- [ ] Implement TLS config with strong defaults (TLS 1.2+, good ciphers)
- [ ] Unit/integration tests

### 6.2 Autoconfig / Autodiscover
- [ ] Implement Thunderbird autoconfig XML endpoint
- [ ] Implement Outlook autodiscover XML endpoint
- [ ] Implement Apple Mail config profile (mobileconfig)
- [ ] Unit tests for each config format

---

## Phase 7: HTTP Server & APIs (Weeks 6-9)

### 7.1 HTTP Server Core
- [ ] Implement `internal/http/server.go` — `net/http` based
- [ ] Route setup (API, SPA, autoconfig, health, metrics)
- [ ] SPA handler (serve index.html for non-file routes)
- [ ] CORS middleware (for dev mode with Vite)
- [ ] JWT auth middleware (issue, verify, refresh)
- [ ] Admin auth middleware (separate token scope)
- [ ] Rate limiting middleware
- [ ] Request logging middleware
- [ ] CSP and security headers

### 7.2 Webmail REST API
- [ ] `GET /api/v1/mail/messages` — list with pagination, sort, filter
- [ ] `GET /api/v1/mail/messages/{id}` — full message with parsed body
- [ ] `GET /api/v1/mail/messages/{id}/raw` — raw RFC 822
- [ ] `GET /api/v1/mail/messages/{id}/attachments/{name}` — download
- [ ] `POST /api/v1/mail/messages` — send / save draft
- [ ] `PUT /api/v1/mail/messages/{id}/flags` — update flags
- [ ] `PUT /api/v1/mail/messages/{id}/move` — move to folder
- [ ] `DELETE /api/v1/mail/messages/{id}` — delete
- [ ] `POST /api/v1/mail/messages/bulk` — bulk operations
- [ ] `GET /api/v1/mail/search` — full-text search
- [ ] `GET /api/v1/folders` — list folders
- [ ] `POST /api/v1/folders` — create folder
- [ ] `PUT /api/v1/folders/{name}` — rename
- [ ] `DELETE /api/v1/folders/{name}` — delete
- [ ] `GET /api/v1/contacts` — contact search
- [ ] `POST /api/v1/compose/send` — send email
- [ ] `POST /api/v1/compose/draft` — save draft
- [ ] `POST /api/v1/compose/attachments` — upload attachment
- [ ] `GET /api/v1/settings` — get user settings
- [ ] `PUT /api/v1/settings` — update settings
- [ ] 2FA endpoints (setup, verify, disable)
- [ ] App password endpoints (create, delete)
- [ ] Unit tests for all API endpoints

### 7.3 Admin REST API
- [ ] Dashboard stats endpoint
- [ ] Domain CRUD endpoints
- [ ] DNS record helper endpoint
- [ ] Account CRUD endpoints
- [ ] Alias CRUD endpoints
- [ ] Queue management endpoints
- [ ] Blocklist endpoints
- [ ] DKIM management endpoints
- [ ] TLS certificate endpoints
- [ ] Log viewer endpoint
- [ ] Unit tests

### 7.4 WebSocket (Real-time)
- [ ] Implement WebSocket endpoint for real-time mail notifications
- [ ] Push new message count to connected webmail clients
- [ ] Push queue status updates to admin panel
- [ ] Auth via ticket (short-lived token exchanged for WS connection)

---

## Phase 8: MCP Server (Weeks 9-10)

- [ ] Implement `internal/mcp/server.go` — JSON-RPC 2.0 over stdio/HTTP
- [ ] Implement tool: `umailserver_send`
- [ ] Implement tool: `umailserver_search`
- [ ] Implement tool: `umailserver_read`
- [ ] Implement tool: `umailserver_list`
- [ ] Implement tool: `umailserver_move`
- [ ] Implement tool: `umailserver_delete`
- [ ] Implement tool: `umailserver_flag`
- [ ] Implement tool: `umailserver_folders`
- [ ] Implement tool: `umailserver_contacts`
- [ ] Implement tool: `umailserver_stats`
- [ ] Implement tool: `umailserver_queue_status` (admin)
- [ ] Implement tool: `umailserver_domain_add` (admin)
- [ ] Implement tool: `umailserver_account_add` (admin)
- [ ] Auth: API token validation
- [ ] Unit tests for each tool
- [ ] Integration test: MCP tool → verify action in mailbox

---

## Phase 9: Webmail Frontend (Weeks 7-12)

### 9.1 Project Setup
- [ ] Initialize React 19 + Vite project in `web/webmail/`
- [ ] Configure Tailwind CSS v4
- [ ] Install and configure shadcn/ui components
- [ ] Set up React Router v7
- [ ] Set up TanStack Query
- [ ] Set up Zustand stores
- [ ] Set up API client (`lib/api.ts`)
- [ ] Configure build output for Go embed

### 9.2 Layout & Navigation
- [ ] 3-panel ResizablePanel layout (sidebar + list + reader)
- [ ] Responsive: collapse to 2-panel on tablet, 1-panel on mobile
- [ ] Sidebar: folder tree with unread counts
- [ ] Sidebar: custom labels section
- [ ] Sidebar: compose button
- [ ] Top bar: search input
- [ ] Dark mode / light mode / system toggle

### 9.3 Mail List
- [ ] Message list with virtual scrolling (TanStack Virtual)
- [ ] Message row: checkbox, star, from, subject, preview, date, attachment icon
- [ ] Multi-select with shift+click
- [ ] Bulk action bar (archive, delete, move, mark read/unread, label)
- [ ] Sort by date/from/subject/size
- [ ] Pull-to-refresh on mobile
- [ ] Loading skeleton states
- [ ] Empty state per folder

### 9.4 Mail Reader
- [ ] Message header display (from, to, cc, date, subject)
- [ ] HTML email rendering (sandboxed, DOMPurify sanitized)
- [ ] Plain text email rendering (with URL linkification)
- [ ] Inline image display
- [ ] Attachment list with download buttons
- [ ] Attachment preview (images, PDFs)
- [ ] Reply / Reply All / Forward buttons
- [ ] Archive / Delete / Move / Label actions
- [ ] Print message
- [ ] View raw source (RFC 822)
- [ ] External image blocking with "load images" button

### 9.5 Conversation/Thread View
- [ ] Group messages by thread (References/In-Reply-To headers)
- [ ] Collapsible message chain
- [ ] Expand/collapse all
- [ ] Reply continues thread

### 9.6 Compose
- [ ] Compose drawer/sheet (slides up from bottom)
- [ ] To / CC / BCC fields with contact autocomplete
- [ ] Subject input
- [ ] TipTap rich text editor
  - [ ] Bold, italic, underline, strikethrough
  - [ ] Bullet/numbered lists
  - [ ] Links
  - [ ] Inline images (paste from clipboard)
  - [ ] Code blocks
  - [ ] Quote blocks
  - [ ] Font color
- [ ] Markdown toggle mode
- [ ] Attachment upload (drag & drop + file picker)
- [ ] Attachment preview with remove button
- [ ] Signature insertion
- [ ] Auto-save drafts (every 30s via TanStack Query mutation)
- [ ] Undo send (configurable delay)
- [ ] ⌘+Enter to send

### 9.7 Search
- [ ] Search bar with instant results (debounced)
- [ ] Advanced search filters: from, to, subject, date range, has:attachment
- [ ] Search results list
- [ ] Search highlighting in results
- [ ] Recent searches

### 9.8 Settings
- [ ] Profile: display name, avatar
- [ ] Signature editor (HTML with preview)
- [ ] Vacation auto-responder (enable/disable, message, date range)
- [ ] Mail forwarding (address, keep copy toggle)
- [ ] Spam threshold slider
- [ ] Keyboard shortcuts reference sheet
- [ ] 2FA setup (QR code, backup codes)
- [ ] App passwords (generate, list, delete)
- [ ] Theme selection (light/dark/system)

### 9.9 Keyboard Shortcuts
- [ ] Implement keyboard shortcut system (hotkeys.js or custom)
- [ ] All Gmail-compatible shortcuts per IMPLEMENTATION.md
- [ ] `?` to show shortcut help overlay
- [ ] ⌘K command palette (shadcn Command component)

### 9.10 Real-time Updates
- [ ] WebSocket connection for new mail notifications
- [ ] Update unread counts in sidebar on new mail
- [ ] Toast notification for new mail
- [ ] Browser notification API (with permission request)
- [ ] Sound notification (optional, configurable)

---

## Phase 10: Admin Panel Frontend (Weeks 10-13)

### 10.1 Project Setup
- [ ] Initialize React 19 + Vite project in `web/admin/`
- [ ] Share Tailwind + shadcn config with webmail
- [ ] Set up routing and API client

### 10.2 Dashboard
- [ ] Server status card (uptime, version, hostname)
- [ ] Mail volume chart (incoming/outgoing, last 24h/7d/30d)
- [ ] Spam/ham ratio pie chart
- [ ] Queue health indicator
- [ ] Storage usage per domain
- [ ] Active connections count
- [ ] Recent errors list

### 10.3 Domain Management
- [ ] Domain list table
- [ ] Add domain form
- [ ] Domain detail page (settings, stats)
- [ ] DNS helper: show all required DNS records with copy buttons
  - [ ] MX record
  - [ ] SPF record
  - [ ] DKIM record (with public key)
  - [ ] DMARC record
  - [ ] MTA-STS record + policy file
  - [ ] DANE/TLSA record
- [ ] DNS verification check (green/red per record)

### 10.4 Account Management
- [ ] Account list table (per domain)
- [ ] Create account form
- [ ] Account detail page (quota, activity, settings)
- [ ] Reset password
- [ ] Enable/disable account
- [ ] Alias management per account

### 10.5 Queue Management
- [ ] Queue list with status filters (pending, failed, delivering)
- [ ] Retry single / retry all failed
- [ ] Drop message from queue
- [ ] View message details (envelope, error log, retry history)

### 10.6 Security
- [ ] IP blocklist management (add, remove, view reason)
- [ ] Currently blocked IPs (brute-force auto-blocks)
- [ ] Rate limit settings editor
- [ ] Recent login attempts log

### 10.7 TLS & Certificates
- [ ] Certificate list (domain, expiry, auto-renew status)
- [ ] Manual renewal trigger
- [ ] Certificate detail (issuer, SANs, fingerprint)

---

## Phase 11: Account Self-Service Portal (Weeks 11-12)

- [ ] Initialize React 19 + Vite project in `web/account/`
- [ ] Login page (email + password)
- [ ] Change password form
- [ ] 2FA setup page (QR code + verification)
- [ ] 2FA disable (with password confirmation)
- [ ] App passwords management
- [ ] Vacation responder settings
- [ ] Mail forwarding settings
- [ ] Quota usage display

---

## Phase 12: CLI Tools & Diagnostics (Weeks 11-13)

### 12.1 Quickstart
- [ ] `umailserver quickstart you@example.com`
  - [ ] Generate default config
  - [ ] Create first admin account
  - [ ] Generate DKIM keys
  - [ ] Print all required DNS records
  - [ ] Start server

### 12.2 Diagnostics
- [ ] `umailserver check dns example.com` — verify all DNS records
- [ ] `umailserver check tls example.com` — TLS configuration test
- [ ] `umailserver check deliverability` — full deliverability audit
- [ ] `umailserver test send user@example.com` — send test email

### 12.3 Backup & Restore
- [ ] `umailserver backup /path/` — full backup (config + DB + Maildir)
- [ ] `umailserver restore /path/` — restore from backup
- [ ] Incremental backup support

### 12.4 Migration
- [ ] `umailserver migrate --source imap://...` — IMAP-to-IMAP sync
- [ ] `umailserver migrate --source dovecot --passwd /path` — Dovecot import
- [ ] `umailserver migrate --source mbox /path/*.mbox` — MBOX import

---

## Phase 13: Security Hardening (Weeks 12-14)

- [ ] Implement `internal/security/ratelimit.go` — token bucket per IP/account
- [ ] Implement `internal/security/bruteforce.go` — auto-block after N failures
- [ ] Implement `internal/security/blocklist.go` — IP/domain block management
- [ ] Fuzz testing for SMTP parser
- [ ] Fuzz testing for IMAP parser
- [ ] Fuzz testing for MIME parser
- [ ] Security audit of HTML email sanitization
- [ ] Ensure no path traversal in Maildir operations
- [ ] Ensure all protocol parsers have bounded reads
- [ ] Add connection timeouts on all listeners

---

## Phase 14: Documentation & Polish (Weeks 13-14)

- [ ] Write comprehensive README.md
- [ ] Write `docs/quickstart.md`
- [ ] Write `docs/configuration.md`
- [ ] Write `docs/dns-setup.md`
- [ ] Write `docs/migration.md`
- [ ] Write `docs/troubleshooting.md`
- [ ] Write `docs/api-reference.md`
- [ ] Write `docs/mcp-integration.md`
- [ ] Write `CONTRIBUTING.md`
- [ ] Write `SECURITY.md`
- [ ] Set up `umailserver.com` docs site
- [ ] Create BRANDING.md (logo, colors, messaging)
- [ ] Create promotional materials (GitHub social preview, etc.)
- [ ] End-to-end test: fresh install → quickstart → send/receive → webmail
- [ ] Performance benchmarks (SMTP throughput, IMAP connections, search speed)
- [ ] Memory profiling under load
- [ ] Tag v1.0.0 release
