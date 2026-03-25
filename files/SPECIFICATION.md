# uMailServer — Unified Mail Server

## Project Identity

| Field | Value |
|-------|-------|
| **Name** | uMailServer |
| **Tagline** | One binary. Complete email. |
| **Language** | Go (1.23+) |
| **Distribution** | Single static binary + embedded React webmail UI |
| **License** | AGPL-3.0 (community) / Commercial (enterprise) |
| **GitHub Org** | `github.com/umailserver` |
| **Main Repo** | `github.com/umailserver/umailserver` |
| **Domain** | `umailserver.com` |
| **Target** | Developers, sysadmins, hosting providers, and SMBs who want a self-hosted mail server without stitching together 6+ separate tools |

---

## Problem Statement

Setting up a self-hosted mail server in 2026 still requires installing, configuring, and maintaining **6-8 separate components**:

| Component | Traditional Stack | What Goes Wrong |
|-----------|------------------|-----------------|
| SMTP (outbound) | Postfix | Complex `main.cf` + `master.cf`, 500+ config directives |
| SMTP (inbound/MX) | Postfix or Exim | Separate config for receiving vs sending |
| IMAP/POP3 | Dovecot | Separate auth, separate config, mailbox format headaches |
| Spam filtering | SpamAssassin | Perl-based, slow, memory hog, rule updates break |
| DKIM signing | OpenDKIM | Separate daemon, key management nightmare |
| SPF/DMARC | OpenDMARC | Yet another daemon, report parsing is manual |
| Antivirus | ClamAV | 1GB+ RAM for virus definitions alone |
| Webmail | Roundcube/RainLoop | Separate PHP app, separate DB, separate config, 2010-era UI |

**Result:** A typical mail server has 6-8 running daemons, 10+ config files in different formats, 3+ databases, and takes 2-4 hours to set up even for experienced sysadmins. Maintenance is a constant burden.

**uMailServer replaces all of this with a single Go binary.**

---

## Competitive Landscape

### Direct Competitors

| Server | Language | SMTP | IMAP | Spam | DKIM/SPF/DMARC | Webmail | Admin UI | Multi-domain | MCP |
|--------|----------|------|------|------|----------------|---------|----------|--------------|-----|
| **Maddy** | Go | ✅ | ⚠️ Beta | ❌ | ✅ | ❌ | ❌ | ✅ | ❌ |
| **mox** | Go | ✅ | ✅ | ✅ Basic | ✅ | ⚠️ Basic Go templates | ⚠️ Basic | ✅ | ❌ |
| **Stalwart** | Rust | ✅ | ✅ | ✅ LLM-powered | ✅ | ❌ (planned 2026, Dioxus) | ✅ Web | ✅ | ❌ |
| **Mail-in-a-Box** | Python/Bash | ✅ | ✅ | ✅ | ✅ | ✅ Roundcube | ✅ | ⚠️ | ❌ |
| **Mailu** | Python (Docker) | ✅ | ✅ | ✅ | ✅ | ✅ Roundcube | ✅ | ✅ | ❌ |
| **uMailServer** | **Go** | ✅ | ✅ | ✅ | ✅ | ✅ **Modern React** | ✅ **Full** | ✅ | ✅ |

### Key Differentiators vs Each Competitor

**vs Maddy:**
- Maddy has no webmail, no admin UI, IMAP is still beta
- uMailServer ships with full React webmail + admin panel
- Maddy's spam filtering is minimal; uMailServer has Bayesian + RBL + heuristic scoring

**vs mox:**
- mox has a webmail but it's Go-native HTML templates — functional but ugly
- uMailServer has a modern React 19 + Tailwind v4 + shadcn/ui webmail that rivals Gmail
- mox has no MCP server integration
- mox config is file-based only; uMailServer has full API + Web Admin

**vs Stalwart:**
- Stalwart is the most feature-rich but written in Rust (different ecosystem)
- Stalwart has NO webmail (planned 2026 with Dioxus — unproven framework)
- Stalwart has JMAP which is powerful but complex; uMailServer focuses on IMAP (universal)
- uMailServer is Go — easier for community contributions than Rust

**vs Mail-in-a-Box / Mailu:**
- These are "glue projects" — they install Postfix + Dovecot + Roundcube + SpamAssassin as separate processes
- uMailServer is one binary, one process, one config
- MIAB only supports Ubuntu; uMailServer runs anywhere Go compiles (Linux, macOS, FreeBSD)

---

## What uMailServer Replaces

| Traditional Tool | uMailServer Module | Improvement |
|------------------|--------------------|-------------|
| Postfix (SMTP) | `smtp` module | Native Go, single config, integrated queue |
| Dovecot (IMAP/POP3) | `imap` module | Integrated auth, no separate daemon |
| SpamAssassin | `spam` module | Bayesian + RBL + heuristic, no Perl dependency |
| ClamAV | `antivirus` module (v2) | Lightweight YARA-based scanning, not 1GB RAM |
| OpenDKIM | `dkim` module | Integrated signing/verification, auto key rotation |
| OpenSPF/OpenDMARC | `auth` module | SPF + DMARC + ARC + MTA-STS in one |
| Roundcube/RainLoop | `webmail` module | React 19 + shadcn/ui, embedded in binary |
| cPanel Mail Config | `admin` module | Full web admin panel, REST API |
| certbot (SSL) | `tls` module | Built-in ACME client, auto-renewal |
| Fail2Ban (mail) | `security` module | Integrated brute-force protection |

---

## Architecture

### High-Level Overview

```
┌─────────────────────────────────────────────────────────────┐
│                      uMailServer Binary                      │
│                                                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │  SMTP    │  │  IMAP    │  │  HTTP    │  │  Admin API  │  │
│  │ :25,:587 │  │ :993,:143│  │ :443,:80 │  │  :8443      │  │
│  │  :465    │  │          │  │          │  │             │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬──────┘  │
│       │              │             │                │         │
│  ┌────┴──────────────┴─────────────┴────────────────┴─────┐  │
│  │                    Message Pipeline                     │  │
│  │  receive → auth → spam → dkim → store → deliver/relay  │  │
│  └────────────────────────┬────────────────────────────────┘  │
│                           │                                   │
│  ┌────────────────────────┴────────────────────────────────┐  │
│  │                     Storage Layer                        │  │
│  │  ┌──────────┐  ┌──────────┐  ┌────────┐  ┌──────────┐  │  │
│  │  │ Mailbox  │  │  Queue   │  │ Config │  │  Index   │  │  │
│  │  │ (files)  │  │ (embedded│  │ (embed │  │ (FTS)    │  │  │
│  │  │          │  │    DB)   │  │   DB)  │  │          │  │  │
│  │  └──────────┘  └──────────┘  └────────┘  └──────────┘  │  │
│  └─────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌──────────────────────────────────────────────────────────┐│
│  │  Embedded UI (embed.FS)                                   ││
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  ││
│  │  │  Webmail    │  │ Admin Panel │  │  Account Self-  │  ││
│  │  │ (React SPA) │  │ (React SPA) │  │  Service Portal │  ││
│  │  └─────────────┘  └─────────────┘  └─────────────────┘  ││
│  └──────────────────────────────────────────────────────────┘│
│                                                              │
│  ┌────────────┐  ┌────────────┐  ┌────────────────────────┐ │
│  │ MCP Server │  │ Metrics    │  │  Webhook / Events      │ │
│  │ (JSON-RPC) │  │ (Prometheus)│  │  (HTTP callbacks)     │ │
│  └────────────┘  └────────────┘  └────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### Module Architecture

uMailServer is built as a set of internal Go packages. Each module has a clean interface and communicates through well-defined internal APIs. **There is no plugin system** — all modules compile into the single binary.

```
cmd/
  umailserver/          # Main entry point, CLI commands
internal/
  smtp/                 # SMTP server (inbound MX + outbound submission)
    server.go           # Listener, connection handler
    session.go          # Per-connection state machine
    queue.go            # Outbound delivery queue with retry
    relay.go            # Remote delivery (MX lookup, TLS, retry backoff)
    pipeline.go         # Message pipeline: receive → check → store/relay
  imap/                 # IMAP4rev2 server
    server.go           # Listener, connection handler
    session.go          # Per-connection state machine, command dispatch
    mailbox.go          # Mailbox operations (SELECT, FETCH, STORE, etc.)
    search.go           # SEARCH/SORT commands
    idle.go             # IDLE push notifications
  pop3/                 # POP3 server (optional, for legacy clients)
    server.go
  auth/                 # Authentication & authorization
    spf.go              # SPF record evaluation
    dkim.go             # DKIM signing and verification
    dmarc.go            # DMARC policy evaluation
    arc.go              # ARC chain validation and sealing
    mtasts.go           # MTA-STS policy fetching and caching
    dane.go             # DANE/TLSA verification
    user.go             # User authentication (password, TOTP 2FA)
  spam/                 # Spam filtering engine
    bayesian.go         # Bayesian classifier (train/classify)
    rbl.go              # RBL/DNSBL lookup (Spamhaus, etc.)
    heuristic.go        # Rule-based heuristic scoring
    greylist.go         # Greylisting implementation
    scorer.go           # Unified spam score aggregation
  store/                # Mail storage
    maildir.go          # Maildir++ format storage
    index.go            # Full-text search index (bleve or custom)
    metadata.go         # Message metadata, flags, labels
  queue/                # Message queue for outbound delivery
    queue.go            # Persistent queue (embedded DB)
    scheduler.go        # Retry scheduler with exponential backoff
    bounce.go           # Bounce/DSN generation
  tls/                  # TLS & certificate management
    acme.go             # ACME client (Let's Encrypt)
    manager.go          # Certificate store, auto-renewal
    sni.go              # SNI-based cert selection for multi-domain
  dns/                  # DNS utilities
    resolver.go         # DNS resolution with caching
    mx.go               # MX record lookup
    autoconfig.go       # Autoconfig/Autodiscover for mail clients
  config/               # Configuration management
    config.go           # YAML/TOML config loader
    domain.go           # Per-domain configuration
    defaults.go         # Sensible defaults
  admin/                # Admin REST API
    api.go              # HTTP handlers
    domain.go           # Domain CRUD
    account.go          # Account CRUD
    queue.go            # Queue management
    stats.go            # Server statistics
  mcp/                  # MCP (Model Context Protocol) server
    server.go           # JSON-RPC transport
    tools.go            # Mail tools (send, search, read, list)
  metrics/              # Observability
    prometheus.go       # Prometheus metrics exporter
    health.go           # Health check endpoint
  security/             # Security features
    ratelimit.go        # Per-IP, per-account rate limiting
    bruteforce.go       # Brute-force detection and blocking
    blocklist.go        # IP/domain blocklist management
web/                    # Frontend (built separately, embedded at compile)
  webmail/              # React webmail SPA
  admin/                # React admin panel SPA
  account/              # React account self-service portal
```

---

## Dependency Policy: Minimal Dependencies

### Allowed External Dependencies (stdlib insufficient)

| Dependency | Reason | Alternative Considered |
|-----------|--------|----------------------|
| `crypto/tls` (stdlib) | TLS — already in stdlib | N/A |
| `golang.org/x/crypto` | Extended crypto (bcrypt, argon2, ed25519) | Can't reasonably reimplement |
| `golang.org/x/net` | IDNA, extended DNS | Too complex to reimplement |
| A YAML/TOML parser | Config file parsing | Could use JSON but UX suffers |
| An embedded KV store (bbolt or similar) | Queue persistence, config store | Could use SQLite but adds CGO |

### NOT Allowed

| Category | Banned | Reason |
|----------|--------|--------|
| Web frameworks | gin, echo, fiber | `net/http` is sufficient |
| ORM | gorm, ent | Direct DB/store access |
| Logging frameworks | zap, logrus | `log/slog` (stdlib) is sufficient |
| DI containers | wire, fx | Manual dependency injection |
| Config libraries | viper, cobra | Only a YAML parser needed |
| Protocol libraries | go-imap, go-smtp | Implement from scratch for full control |

### Build Constraints

- `CGO_ENABLED=0` — pure Go, no C dependencies
- Static binary — single file deployment
- Cross-compilation for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, freebsd/amd64

---

## Protocol Specifications

### SMTP (RFC 5321 + Extensions)

**Inbound (MX — port 25):**
- EHLO/HELO handshake
- STARTTLS (RFC 3207) — required for authenticated sessions
- MAIL FROM / RCPT TO with size limits
- DATA with message pipeline processing
- 8BITMIME (RFC 6152)
- SMTPUTF8 (RFC 6531) — internationalized email
- CHUNKING/BDAT (RFC 3030)
- PIPELINING (RFC 2920)

**Outbound (Submission — port 587/465):**
- Authentication required (PLAIN, LOGIN, CRAM-MD5 over TLS)
- Message submission → queue → delivery
- Implicit TLS on port 465 (RFC 8314)
- STARTTLS on port 587

**Delivery Pipeline:**
```
Receive → Rate Limit Check → SPF Check → DKIM Verify → DMARC Evaluate
→ Spam Score → Greylist Check → Virus Scan (v2) → Store to Mailbox
                                                   → or Reject/Quarantine
```

**Outbound Pipeline:**
```
Submit → Authenticate → Rate Limit → DKIM Sign → Queue
→ MX Lookup → TLS Negotiate (DANE/MTA-STS) → Deliver
→ on failure: Retry (exponential backoff) → Bounce after N retries
```

### IMAP4rev2 (RFC 9051)

**Core Commands:**
- CAPABILITY, NOOP, LOGOUT
- AUTHENTICATE (PLAIN, OAUTHBEARER), LOGIN
- SELECT, EXAMINE, CREATE, DELETE, RENAME, SUBSCRIBE, UNSUBSCRIBE
- LIST, STATUS, APPEND
- CHECK, CLOSE, EXPUNGE
- SEARCH, FETCH, STORE, COPY, MOVE, UID variants

**Extensions:**
- IDLE (RFC 2177) — real-time push notifications
- CONDSTORE/QRESYNC (RFC 7162) — efficient sync
- SPECIAL-USE (RFC 6154) — well-known folders (Inbox, Sent, Drafts, Trash, Junk)
- LITERAL+ (RFC 7888)
- MOVE (RFC 6851)
- NAMESPACE (RFC 2342)
- ID (RFC 2971)
- SORT/THREAD (RFC 5256)
- COMPRESS (RFC 4978) — DEFLATE compression

### POP3 (RFC 1939) — Optional

- Basic TOP, RETR, DELE, LIST, STAT
- UIDL for unique message IDs
- STLS for STARTTLS
- Primarily for legacy client compatibility

---

## Spam Filtering Engine

### Multi-Layer Scoring System

Each incoming message gets a spam score (0.0 = clean, 10.0+ = definite spam):

| Layer | Weight | Description |
|-------|--------|-------------|
| **RBL/DNSBL** | 0–4.0 | Check sender IP against Spamhaus ZEN, Barracuda, SpamCop, etc. |
| **SPF** | 0–2.0 | SPF fail = +2.0, softfail = +1.0, pass = 0 |
| **DKIM** | 0–2.0 | DKIM fail = +1.5, missing = +0.5 (for domains with DKIM policy) |
| **DMARC** | 0–2.0 | DMARC fail = +2.0, none policy = +0.0 |
| **Heuristic Rules** | 0–5.0 | Pattern matching: ALL CAPS subject, excessive URLs, HTML-only, etc. |
| **Bayesian** | -3.0–5.0 | Trained per-user classifier (ham/spam corpus) |
| **Greylisting** | 0–1.0 | First-time sender+recipient+IP triplet gets temporary reject |
| **Reputation** | -2.0–3.0 | Sender domain/IP reputation based on history |

**Thresholds (configurable per-domain):**
- Score < 3.0 → Inbox
- Score 3.0–6.0 → Junk folder
- Score 6.0–9.0 → Quarantine (admin review)
- Score ≥ 9.0 → Reject at SMTP level

### Bayesian Classifier

- Per-user training corpus stored in mailbox metadata
- User marks messages as spam/ham → classifier updates
- Token-based (word + bigram) with TF-IDF weighting
- Auto-training: messages in Junk folder = spam, messages in Inbox > 7 days = ham
- Exportable/importable training data per domain

---

## Authentication & Security

### Email Authentication Chain

```
Incoming Message
├── SPF: Check sending IP against domain's SPF record
├── DKIM: Verify cryptographic signature on headers/body
├── DMARC: Apply domain owner's policy (none/quarantine/reject)
├── ARC: Validate ARC chain for forwarded messages
├── DANE: Verify TLS certificate against TLSA DNS record
└── MTA-STS: Enforce TLS policy per receiving domain's MTA-STS record
```

### DKIM Key Management

- Auto-generate 2048-bit RSA keys per domain on setup
- Support Ed25519 keys (RFC 8463)
- Auto key rotation every 90 days (configurable)
- Publish DNS records via admin UI (copy-paste ready)
- Multi-selector support (default, marketing, transactional)

### User Authentication

- Password storage: Argon2id
- TOTP 2FA support (RFC 6238)
- App passwords (separate per-application passwords for IMAP/SMTP clients)
- OAuth 2.0 / OIDC integration (optional, for SSO)
- Rate-limited login attempts (brute-force protection)
- IP blocklist for repeated failed attempts

### TLS Configuration

- Built-in ACME client for Let's Encrypt (HTTP-01 and DNS-01 challenges)
- Auto-renewal (30 days before expiry)
- SNI-based certificate selection for multi-domain
- Minimum TLS 1.2, prefer TLS 1.3
- Strong cipher suite defaults (no RC4, no 3DES, no CBC where avoidable)
- OCSP stapling

---

## Webmail UI (React SPA)

### Technology Stack

| Layer | Technology |
|-------|-----------|
| Framework | React 19 |
| Styling | Tailwind CSS v4 |
| Components | shadcn/ui |
| Icons | Lucide React |
| State | Zustand |
| Router | React Router v7 |
| Rich Text Editor | TipTap (email composer) |
| Data Fetching | TanStack Query |
| Bundler | Vite |

### Layout

Gmail-inspired 3-panel layout using shadcn `ResizablePanel`:

```
┌─────────────┬──────────────────────┬──────────────────────┐
│  Sidebar    │  Mail List           │  Mail Reader         │
│             │                      │                      │
│  Inbox (12) │  ☐ Subject line...   │  From: sender@...    │
│  Sent       │    Preview text      │  To: me@...          │
│  Drafts (2) │    2h ago            │  Date: ...           │
│  Junk       │  ────────────────    │                      │
│  Trash      │  ☐ Subject line...   │  Email body content  │
│  Archive    │    Preview text      │  rendered as HTML    │
│             │    yesterday         │  with safe sanitize  │
│  Labels:    │  ────────────────    │                      │
│  • Work     │  ☐ Subject line...   │  [Reply] [Forward]   │
│  • Personal │    Preview text      │  [Archive] [Delete]  │
│             │    3 days ago        │                      │
│  [+] Label  │                      │  Attachments:        │
│             │                      │  📎 file.pdf (2.4MB) │
└─────────────┴──────────────────────┴──────────────────────┘
```

### Core Features

**Email Management:**
- Inbox, Sent, Drafts, Junk, Trash, Archive — IMAP SPECIAL-USE folders
- Custom labels/tags with colors (stored as IMAP keywords)
- Star/flag messages
- Bulk select + actions (archive, delete, move, label)
- Conversation/thread view (collapsible message chains)
- Full-text search with filters (from, to, subject, date range, has:attachment)
- Keyboard shortcuts (Gmail-compatible: `e` archive, `#` delete, `r` reply, `c` compose)
- Drag & drop messages between folders
- Swipe gestures on mobile (left=archive, right=delete)

**Email Composer:**
- Rich text editor (TipTap with email-safe HTML output)
- Markdown mode toggle
- Inline image paste (clipboard → attachment → `<img>` tag)
- File attachments with drag & drop
- CC/BCC toggle
- Email templates / signatures (per-account)
- Auto-save drafts every 30 seconds
- Reply / Reply All / Forward
- Undo send (configurable 5-30 second delay)
- Contact autocomplete from address book

**Settings:**
- Signature editor (HTML)
- Vacation auto-responder
- Mail forwarding rules
- Spam threshold adjustment
- 2FA setup (QR code)
- App password management
- Theme: light/dark/system

### Admin Panel (Separate React SPA)

**Dashboard:**
- Server status (uptime, memory, connections)
- Mail queue size and health
- Incoming/outgoing mail volume charts
- Spam/ham ratio
- Top senders/recipients
- Storage usage per domain

**Domain Management:**
- Add/remove domains
- DNS record helper (shows required MX, SPF, DKIM, DMARC records)
- Per-domain settings (spam threshold, max mailbox size, etc.)
- DKIM key management (generate, rotate, view DNS record)

**Account Management:**
- Create/delete accounts
- Set quotas (mailbox size, send rate)
- Reset passwords
- Enable/disable 2FA
- View account activity log
- Alias management

**Queue Management:**
- View outbound queue
- Retry failed deliveries
- Remove stuck messages
- Bounce management

**Security:**
- IP blocklist/allowlist
- View blocked IPs (brute-force)
- Rate limit settings
- TLS certificate status

### Account Self-Service Portal

- Change password
- Set up 2FA
- Manage app passwords
- Configure vacation responder
- Set mail forwarding
- View quota usage

---

## MCP Server Integration

### Available Tools

```
umailserver_send          — Send an email
umailserver_search        — Search emails (from, to, subject, body, date)
umailserver_read          — Read a specific email by ID
umailserver_list          — List emails in a folder (inbox, sent, etc.)
umailserver_move          — Move email(s) to a folder
umailserver_delete        — Delete email(s)
umailserver_flag          — Star/flag an email
umailserver_folders       — List all folders
umailserver_contacts      — List/search contacts from address book
umailserver_stats         — Server statistics
umailserver_queue_status  — Outbound queue status (admin)
umailserver_domain_add    — Add a domain (admin)
umailserver_account_add   — Create an account (admin)
```

### Example Usage

```
User: "Send an email to john@example.com about the meeting tomorrow"
Claude → MCP: umailserver_send(to="john@example.com", subject="Meeting Tomorrow", body="...")

User: "Summarize my unread emails"
Claude → MCP: umailserver_list(folder="inbox", unread=true, limit=20)
Claude: "You have 20 unread emails. Here's a summary..."

User: "Find all emails from Alice about the project"
Claude → MCP: umailserver_search(from="alice@", subject="project")
```

---

## Storage Design

### Mailbox Format: Maildir++

- One directory per mailbox
- One file per message (crash-safe, no locking needed)
- Subdirectories for IMAP folders
- Dovecot-compatible naming for migration

```
/var/lib/umailserver/
  domains/
    example.com/
      users/
        john/
          Maildir/
            new/              # Undelivered messages
            cur/              # Read messages
            tmp/              # In-progress deliveries
            .Sent/            # Sent folder
              new/ cur/ tmp/
            .Drafts/
            .Junk/
            .Trash/
            .Archive/
```

### Embedded Database (bbolt or similar)

Used for non-mailbox data:

- **Queue store:** Outbound message queue with retry metadata
- **Config store:** Domain and account configuration
- **Index store:** Full-text search index for each mailbox
- **Session store:** Active IMAP/SMTP sessions
- **Spam store:** Bayesian classifier training data per user
- **Rate limit store:** Per-IP, per-account counters
- **Metrics store:** Time-series data for admin dashboard

---

## Configuration

### Single Config File: `umailserver.yaml`

```yaml
# uMailServer Configuration
server:
  hostname: mail.example.com
  data_dir: /var/lib/umailserver
  
tls:
  acme:
    enabled: true
    email: admin@example.com
    provider: letsencrypt       # or letsencrypt-staging for testing
    challenge: http-01          # or dns-01
  # Or manual certificates:
  # cert_file: /etc/ssl/mail.pem
  # key_file: /etc/ssl/mail.key

smtp:
  inbound:
    port: 25
    max_message_size: 50MB
    max_recipients: 100
  submission:
    port: 587
    require_auth: true
    require_tls: true
  submission_tls:
    port: 465                    # Implicit TLS
  
imap:
  port: 993                      # Implicit TLS
  starttls_port: 143
  idle_timeout: 30m

pop3:
  enabled: false
  port: 995

http:
  port: 443
  http_port: 80                  # Redirect to HTTPS + ACME challenges
  
admin:
  port: 8443
  bind: 127.0.0.1               # Admin panel on localhost only by default

spam:
  reject_threshold: 9.0
  junk_threshold: 3.0
  rbl_servers:
    - zen.spamhaus.org
    - b.barracudacentral.org
  greylisting:
    enabled: true
    delay: 5m

security:
  max_login_attempts: 5
  lockout_duration: 15m
  rate_limit:
    smtp_per_minute: 30
    imap_connections: 50

mcp:
  enabled: true
  port: 3000
  auth_token: ""                 # Auto-generated on first run

domains:
  - name: example.com
    max_accounts: 100
    max_mailbox_size: 5GB
    dkim:
      selector: default
      # Key auto-generated on domain creation
```

### CLI Commands

```bash
# Initial setup
umailserver quickstart you@example.com
# → Generates config, creates first account, prints DNS records

# Service management
umailserver serve                    # Start server (foreground)
umailserver serve --daemon           # Start as daemon
umailserver status                   # Show server status

# Domain management
umailserver domain add example.com
umailserver domain list
umailserver domain dns example.com   # Print required DNS records

# Account management
umailserver account add john@example.com
umailserver account password john@example.com
umailserver account list example.com
umailserver account delete john@example.com

# Queue management
umailserver queue list
umailserver queue retry <message-id>
umailserver queue flush
umailserver queue drop <message-id>

# Diagnostics
umailserver check dns example.com    # Verify DNS records
umailserver check tls example.com    # Test TLS configuration
umailserver check deliverability     # Run deliverability test
umailserver test send john@example.com  # Send test email

# Backup / Restore
umailserver backup /path/to/backup
umailserver restore /path/to/backup

# Version
umailserver version
```

---

## Versioned Evolution

### v1.0 — Full Stack (This Spec)

Everything described in this document:
- SMTP (inbound + outbound + submission)
- IMAP4rev2
- POP3 (basic)
- Spam filtering (Bayesian + RBL + heuristic + greylist)
- DKIM/SPF/DMARC/ARC/MTA-STS/DANE
- Webmail (React 19 + shadcn/ui)
- Admin Panel
- Account Self-Service
- MCP Server
- ACME/TLS auto
- CLI tools
- Single binary, single config

### v2.0 — Enterprise & Scale

- Multi-node clustering (Raft consensus for config, gossip for health)
- Shared storage backend (S3-compatible for mail blobs)
- LDAP/Active Directory integration
- JMAP protocol support (alongside IMAP)
- Calendar + Contacts (CalDAV/CardDAV)
- Antivirus (YARA-based lightweight scanning)
- DMARC aggregate report generation and parsing
- White-label support for hosting providers
- Webhook events (new mail, bounce, spam, etc.)

### v3.0 — Intelligence

- AI-powered spam detection (local LLM inference or API)
- AI email summarization in webmail
- Smart categorization (auto-labels)
- Predictive unsubscribe suggestions
- Email template AI generation
- Natural language search

---

## Performance Targets

| Metric | Target |
|--------|--------|
| SMTP inbound throughput | 10,000 messages/minute (single node) |
| IMAP concurrent connections | 10,000 |
| Webmail page load | < 1 second (initial), < 200ms (subsequent) |
| Memory usage (idle, 100 accounts) | < 100MB |
| Memory usage (active, 10K accounts) | < 2GB |
| Binary size | < 50MB (with embedded UI) |
| Startup time | < 2 seconds |
| TLS handshake | < 50ms |
| Full-text search (100K messages) | < 500ms |

---

## Deliverability Toolkit

Since IP reputation is the #1 challenge for self-hosted mail, uMailServer includes built-in tools:

### `umailserver check deliverability`

Runs a comprehensive deliverability audit:
1. Reverse DNS (PTR) record check
2. IP blocklist check (Spamhaus, Barracuda, SpamCop, etc.)
3. SPF record validation
4. DKIM key verification
5. DMARC policy check
6. MTA-STS policy check
7. TLS configuration test
8. Test email to a verification service
9. Score report with actionable fixes

### Warm-up Mode

For new mail servers on fresh IPs:
- Gradually increase outbound send rate over 2-4 weeks
- Auto-throttle based on bounce rates
- Guide through feedback loop registration (Gmail, Microsoft, Yahoo)
- Monitor blocklist status

### DMARC Reporting

- Receive and parse aggregate reports (RUA)
- Dashboard showing authentication pass/fail rates per sending source
- Alert on sudden authentication failures

---

## Migration Support

### Import From

| Source | Method |
|--------|--------|
| Postfix + Dovecot | Maildir direct copy + user import from passwd/DB |
| cPanel | cPanel backup file parser |
| Roundcube contacts | CardDAV/CSV import |
| Gmail (Google Takeout) | MBOX import |
| Outlook/Exchange | IMAP sync (IMAP-to-IMAP migration tool) |
| Maddy | Maildir compatible, direct copy |
| mox | Maildir compatible, direct copy |
| Any IMAP server | `umailserver migrate --source imap://old-server` |

---

## Non-Functional Requirements

### Security
- All passwords stored with Argon2id
- No plaintext auth without TLS
- Memory-safe Go (no buffer overflows)
- Input validation on all protocol parsers
- Sandboxed message rendering in webmail (DOMPurify)
- CSP headers on all web endpoints
- Regular dependency audit

### Reliability
- Crash-safe Maildir storage (atomic file operations)
- Queue persistence (survives restart)
- Graceful shutdown (drain connections)
- Health check endpoints
- Watchdog timer for self-restart

### Observability
- Prometheus metrics endpoint (`/metrics`)
- Structured logging (JSON, `log/slog`)
- Request tracing (correlation IDs)
- Admin dashboard with real-time stats

---

## Project Structure Summary

```
umailserver/
├── cmd/umailserver/         # CLI entry point
├── internal/                # All Go packages (private)
│   ├── smtp/                # SMTP server
│   ├── imap/                # IMAP server
│   ├── pop3/                # POP3 server
│   ├── auth/                # SPF/DKIM/DMARC/ARC/MTA-STS/DANE
│   ├── spam/                # Spam filtering engine
│   ├── store/               # Maildir + index storage
│   ├── queue/               # Outbound message queue
│   ├── tls/                 # ACME + cert management
│   ├── dns/                 # DNS utilities + autoconfig
│   ├── config/              # Configuration management
│   ├── admin/               # Admin REST API
│   ├── mcp/                 # MCP server
│   ├── metrics/             # Prometheus + health
│   └── security/            # Rate limiting, brute-force protection
├── web/                     # Frontend source (not shipped, compiled into binary)
│   ├── webmail/             # React webmail SPA
│   ├── admin/               # React admin panel SPA
│   └── account/             # React self-service portal
├── embed.go                 # embed.FS for compiled frontend
├── go.mod
├── go.sum
├── umailserver.yaml.example # Example configuration
├── Dockerfile
├── Makefile
├── SPECIFICATION.md          # This file
├── IMPLEMENTATION.md         # Implementation details (next)
├── TASKS.md                  # Task breakdown (next)
├── BRANDING.md               # Logo, colors, messaging (next)
└── README.md
```
