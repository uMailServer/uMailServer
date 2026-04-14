# uMailServer Architecture

> **Complete architectural documentation for uMailServer** — A production-ready, RFC-compliant email server written in Go.

---

## Table of Contents

1. [Overview](#1-overview)
2. [High-Level Architecture](#2-high-level-architecture)
3. [Component Architecture](#3-component-architecture)
4. [Data Flow Diagrams](#4-data-flow-diagrams)
5. [Storage Architecture](#5-storage-architecture)
6. [Protocol Stack](#6-protocol-stack)
7. [Security Architecture](#7-security-architecture)
8. [RFC Compliance Matrix](#8-rfc-compliance-matrix)
9. [Directory Structure](#9-directory-structure)
10. [Key Design Patterns](#10-key-design-patterns)
11. [Implementation History](#11-implementation-history)

---

## 1. Overview

uMailServer is a **single-binary monolith** email server implementing:

| Protocol | Port | Purpose |
|----------|------|---------|
| SMTP | 25 | Mail Transfer (MX) |
| SMTP Submission | 587 | Mail Submission Agent (MSA) |
| SMTP Submission (TLS) | 465 | Implicit TLS submission |
| IMAP4rev1 | 143 | Mail Access |
| IMAP4rev2 | 143 | Mail Access (newer variant) |
| POP3 | 110 | Mail Access (legacy) |
| POP3 (TLS) | 995 | Implicit TLS pop3 |
| HTTP | 8080 | REST API & Webmail |
| MCP | 3000 | Model Context Protocol (AI) |
| ManageSieve | 4190 | Sieve Script Management |

### Design Principles

- **Single Binary**: All services compile into one executable
- **Embedded Frontends**: React webmail/admin panels embedded via `//go:embed`
- **Minimal Dependencies**: Only purpose-specific libraries (bbolt, jwt, bcrypt, yaml)
- **RFC Compliant**: Full compliance with email standards
- **Production Ready**: TLS, authentication, spam filtering, antivirus scanning

---

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              uMailServer Binary                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐                │
│  │   SMTP    │   │   IMAP    │   │   POP3    │   │   HTTP    │                │
│  │  Server   │   │  Server   │   │  Server   │   │   Server   │                │
│  │  (port 25)│   │  (port 143)│   │  (port 110)│   │ (port 8080)│                │
│  └────┬─────┘   └────┬─────┘   └────┬─────┘   └────┬─────┘                │
│       │              │              │              │                       │
│       └──────────────┴──────────────┴──────────────┘                       │
│                                    │                                        │
│                            ┌────────▼────────┐                              │
│                            │     Server      │                              │
│                            │  Orchestrator   │                              │
│                            │                 │                              │
│                            │  ┌───────────┐  │                              │
│                            │  │ Deliver   │  │                              │
│                            │  │ Pipeline  │  │                              │
│                            │  └───────────┘  │                              │
│                            └────────┬────────┘                              │
│                                     │                                        │
│       ┌──────────────────────────────┼──────────────────────────────┐        │
│       │                              │                              │        │
│  ┌────▼────┐   ┌────────────▼┐   ┌────▼────┐   ┌────────▼────────┐        │
│  │  Queue  │   │   Search    │   │ Sieve   │   │    Storage     │        │
│  │ Manager │   │  Service    │   │Manager  │   │  (bbolt/Maildir)│        │
│  └────┬────┘   └──────┬─────┘   └────┬────┘   └────────┬────────┘        │
│       │                │                │                 │                  │
│       └────────────────┴────────────────┴─────────────────┘                  │
│                                    │                                        │
│                            ┌────────▼────────┐                              │
│                            │     Database     │                              │
│                            │    (bbolt DB)    │                              │
│                            └─────────────────┘                              │
│                                                                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                              Embedded Frontends                             │
│  ┌──────────────────────┐  ┌──────────────────────┐  ┌──────────────────┐    │
│  │   Webmail (React)    │  │   Admin Panel (React)│  │ Account Panel    │    │
│  │   webmail/dist/      │  │    web/admin/dist/   │  │ web/account/dist/│    │
│  └──────────────────────┘  └──────────────────────┘  └──────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Component Architecture

### 3.1 Server Orchestrator (`internal/server/server.go`)

The `Server` struct is the central coordinator:

```go
type Server struct {
    config        *config.Config
    logger        *slog.Logger
    database      *db.DB           // Accounts/domains database
    queue         *queue.Manager  // Outbound queue
    msgStore      *storage.MessageStore  // Maildir++ file storage
    smtpServer    *smtp.Server    // MX server (port 25)
    imapServer    *imap.Server     // IMAP server (port 143)
    pop3Server    *pop3.Server     // POP3 server (port 110)
    apiServer     *api.Server      // HTTP API (port 8080)
    tlsManager    *tls.Manager     // TLS certificates
    webhookMgr    *webhook.Manager // Outbound webhooks
    searchSvc     *search.Service  // Full-text search
    sieveManager  *sieve.Manager   // Sieve filtering
    storageDB     *storage.Database // Message metadata (bbolt)
    mailstore     *imap.BboltMailstore // IMAP mailbox storage
    healthMonitor *health.Monitor  // Health checks
    // ...
}
```

**Startup Sequence:**
```
1. Load config (YAML)
2. Initialize logging
3. Open accounts database (bbolt)
4. Initialize TLS manager
5. Create webhook manager
6. Initialize storage database (mail metadata)
7. Initialize search service
8. Initialize health monitor
9. Start queue manager
10. Create IMAP mailstore (shared storage!)
11. Start SMTP servers (port 25, 587, 465)
12. Start search indexing workers
13. Start IMAP server
14. Start POP3 server
15. Start MCP server
16. Start HTTP API server
```

### 3.2 SMTP Server (`internal/smtp/`)

**Pipeline Pattern for Message Processing:**

```
┌─────────────────────────────────────────────────────────────────┐
│                     SMTP Pipeline Stages                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐           │
│  │   SPF   │──▶│  DKIM   │──▶│ DMARC   │──▶│  GREY   │           │
│  │ Checker │   │ Verify  │   │ Check   │   │  List   │           │
│  └─────────┘   └─────────┘   └─────────┘   └─────────┘           │
│       │                                            │              │
│       │         ┌─────────┐   ┌─────────┐   ┌─────────┐           │
│       └────────▶│   RBL   │──▶│SPAM HEU-──▶│   AV    │           │
│                 │ Checker │   │  RISTICS│   │Scanner │           │
│                 └─────────┘   └─────────┘   └─────────┘           │
│                                              │                     │
│                                              ▼                     │
│                                    ┌───────────────┐            │
│                                    │ deliverLocal  │            │
│                                    │   or relay    │            │
│                                    └───────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

**Stages:**
| Stage | Purpose | RFC |
|-------|---------|-----|
| SPF | Verify sender IP authorization | RFC 7208 |
| DKIM | Verify cryptographic signature | RFC 6376 |
| DMARC | Policy alignment check | RFC 7489 |
| Greylist | Delay first-time senders | RFC 6647 |
| RBL | DNS blacklist checking | RFC 5782 |
| Heuristic | Bayesian spam scoring | - |
| AV | ClamAV virus scanning | - |

### 3.3 IMAP Server (`internal/imap/`)

**Mailbox Storage Backend:**

```
┌─────────────────────────────────────────────────────────────────┐
│                     IMAP Server Architecture                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Client Connection                                              │
│        │                                                         │
│        ▼                                                         │
│   ┌─────────┐                                                   │
│   │ Session │  (per-connection state)                            │
│   └────┬────┘                                                   │
│        │                                                         │
│        ▼                                                         │
│   ┌─────────────────┐                                            │
│   │  Command        │  (SELECT, FETCH, STORE, SEARCH, etc.)     │
│   │  Handler        │                                            │
│   └────────┬────────┘                                            │
│            │                                                      │
│            ▼                                                      │
│   ┌─────────────────────────────────────────┐                   │
│   │         BboltMailstore                   │                   │
│   │  ┌──────────────┐  ┌─────────────────┐   │                   │
│   │  │ bbolt DB    │  │ MessageStore    │   │                   │
│   │  │ (metadata)  │  │ (Maildir++)    │   │                   │
│   │  │             │  │                │   │                   │
│   │  │ • Mailboxes  │  │ user/           │   │                   │
│   │  │ • Messages  │  │   new/          │   │                   │
│   │  │ • Flags     │  │   cur/          │   │                   │
│   │  │ • UIDs      │  │   tmp/          │   │                   │
│   │  └──────────────┘  └─────────────────┘   │                   │
│   └─────────────────────────────────────────┘                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Key IMAP Extensions Supported:**
| Extension | RFC | Status |
|-----------|-----|--------|
| IDLE | RFC 2177 | ✅ Push notifications |
| NAMESPACE | RFC 2342 | ✅ Personal/shared/public |
| MOVE | RFC 6851 | ✅ Server-side move |
| CONDSTORE | RFC 7162 | ✅ Efficient resync |
| QRESYNC | RFC 7162 | ✅ Quick resync |
| SORT | RFC 5256 | ✅ Server-side sorting |
| THREAD | RFC 5256 | ✅ Message threading |
| COMPRESS | RFC 4978 | ✅ DEFLATE compression |
| SPECIAL-USE | RFC 6154 | ✅ \Sent, \Drafts, etc. |
| UIDPLUS | RFC 4315 | ✅ UID expansion |
| ENABLE | RFC 5161 | ✅ Capability enabling |

### 3.4 Storage Architecture (`internal/storage/`)

**Dual-Storage Design:**

```
┌─────────────────────────────────────────────────────────────────┐
│                     Storage Architecture                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │              bbolt Database (mail.db)                   │   │
│   │                                                          │   │
│   │   Buckets:                                               │   │
│   │   ┌─────────────────┐  ┌─────────────────┐              │   │
│   │   │ msgs:{user}:{mb}│  │mailbox:{user}:{mb}│            │   │
│   │   │     (messages)  │  │   (metadata)     │              │   │
│   │   │                 │  │                  │              │   │
│   │   │  UID → JSON     │  │  name, uidnext,  │              │   │
│   │   │                 │  │  uidvalidity    │              │   │
│   │   └─────────────────┘  └─────────────────┘              │   │
│   │                                                          │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                    │                             │
│                                    │  Shared via                │
│                                    │  NewBboltMailstoreWith     │                             │
│                                    │  Interfaces()               │
│                                    ▼                             │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │              Maildir++ File Storage (./data/mail/)      │   │
│   │                                                          │   │
│   │   user@domain.com/                                        │   │
│   │   ├── INBOX/                                             │   │
│   │   │   ├── new/  (received, unseen)                        │   │
│   │   │   ├── cur/  (received, seen)                         │   │
│   │   │   └── tmp/  (temporary)                             │   │
│   │   ├── Sent/                                              │   │
│   │   ├── Drafts/                                            │   │
│   │   ├── Trash/                                             │   │
│   │   └── Junk/                                              │   │
│   │                                                          │   │
│   │   File naming: {hash}{subhash}:2,{flags}                │   │
│   │   Example: d41d8cd98f00b204e9800998ecf8427e:2,S        │   │
│   │                                                          │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Message File Path Structure:**
```
./data/mail/messages/{user}/{first2}/{next2}/{messageID}
Example: ./data/mail/messages/demo@localhost/86/47/8647b19daae2bddfcf7352...
```

### 3.5 HTTP API Server (`internal/api/`)

**REST API Architecture:**

```
┌─────────────────────────────────────────────────────────────────┐
│                      HTTP API Architecture                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   HTTP Requests                                                    │
│        │                                                         │
│        ▼                                                         │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │              Middleware Chain                           │   │
│   │  rateLimit → limitBody → cors → auth → admin         │   │
│   └─────────────────────────────────────────────────────────┘   │
│                            │                                     │
│                            ▼                                     │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                    Route Handler                          │   │
│   │                                                          │   │
│   │   /api/v1/auth/*     → handleLogin, handleRefresh      │   │
│   │   /api/v1/mail/*     → MailHandler (inbox, sent, etc.) │   │
│   │   /api/v1/domains/*  → handleDomains                    │   │
│   │   /api/v1/accounts/* → handleAccounts                    │   │
│   │   /api/v1/queue/*   → handleQueue                       │   │
│   │   /api/v1/filters/*  → handleFilters (Sieve)            │   │
│   │   /api/v1/vacation   → handleVacation                    │   │
│   │   /api/v1/search     → handleSearch                      │   │
│   │   /api/v1/push/*    → handlePushSubscription            │   │
│   │   /api/v1/threads/* → handleThreads                     │   │
│   │                                                          │   │
│   └─────────────────────────────────────────────────────────┘   │
│                            │                                     │
│                            ▼                                     │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                   Storage Layer                            │   │
│   │                                                          │   │
│   │   ┌─────────────┐  ┌──────────────────┐                  │   │
│   │   │  msgStore  │  │    storageDB     │                  │   │
│   │   │(Maildir++) │  │    (bbolt)      │                  │   │
│   │   └─────────────┘  └──────────────────┘                  │   │
│   │                                                          │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**API Endpoints:**

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/auth/login` | POST | Authenticate user |
| `/api/v1/auth/refresh` | POST | Refresh JWT token |
| `/api/v1/mail/{folder}` | GET | List emails in folder |
| `/api/v1/mail/send` | POST | Send email |
| `/api/v1/mail/delete` | DELETE | Delete email |
| `/api/v1/domains` | GET/POST | List/create domains |
| `/api/v1/accounts` | GET/POST | List/create accounts |
| `/api/v1/queue` | GET | View outbound queue |
| `/api/v1/filters` | GET/POST | Manage Sieve filters |
| `/api/v1/vacation` | GET/POST | Auto-reply settings |
| `/api/v1/search` | GET | Full-text search |
| `/api/v1/push/*` | POST | WebPush subscriptions |

---

## 4. Data Flow Diagrams

### 4.1 Inbound Email Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Inbound Email Flow                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  External MTA                                                                │
│       │                                                                      │
│       ▼                                                                      │
│  ┌─────────┐  TCP 25                                                          │
│  │  SMTP   │──────────────────────────────────────────────────────────────┐  │
│  │ Server  │                                                              │  │
│  └────┬────┘                                                              │  │
│       │                                                                     │  │
│       ▼                                                                     │  │
│  ┌─────────────────────────────────────────────────────────────────────┐  │  │
│  │                     Message Pipeline                                   │  │  │
│  │                                                                      │  │  │
│  │  ┌─────┐   ┌─────┐   ┌─────┐   ┌─────┐   ┌─────┐   ┌─────┐        │  │  │
│  │  │HELO/│──▶│AUTH │──▶│MAIL │──▶│RCPT │──▶│DATA │──▶│QUEUE │        │  │  │
│  │  │EHLO │   │     │   │ FROM│   │ TO  │   │     │   │     │        │  │  │
│  │  └─────┘   └─────┘   └─────┘   └─────┘   └─────┘   └──┬──┘        │  │  │
│  │                                                         │            │  │  │
│  │  After DATA:                                            │            │  │  │
│  │  ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐   │            │  │  │
│  │  │SPF Check│──▶│DKIM     │──▶│DMARC   │──▶│Greylist │──▶│            │  │  │
│  │  │         │   │Verify   │   │Check   │   │         │   │            │  │  │
│  │  └─────────┘   └─────────┘   └─────────┘   └─────────┘   │            │  │  │
│  │                                                        │            │  │  │
│  │  ┌─────────┐   ┌─────────┐   ┌─────────┐               │            │  │  │
│  │  │RBL Check│──▶│ Spam    │──▶│ AV Scan │               │            │  │  │
│  │  │         │   │Score   │   │         │               │            │  │  │
│  │  └─────────┘   └─────────┘   └─────────┘               │            │  │  │
│  │                                                        │            │  │  │
│  └────────────────────────────────────────────────────────│────────────┘  │  │
│                                                             │               │  │
│                                                             ▼               │  │
│                                          ┌─────────────────────────────┐  │  │
│                                          │     deliverLocal()          │  │  │
│                                          │                             │  │  │
│                                          │  1. Check user exists       │  │  │
│                                          │  2. Check quota            │  │  │
│                                          │  3. Handle forwarding      │  │  │
│                                          │  4. Store message          │  │  │
│                                          │  5. Index for search      │  │  │
│                                          │  6. Store metadata        │  │  │
│                                          │  7. Trigger webhooks      │  │  │
│                                          └─────────────┬───────────────┘  │  │
│                                                        │                   │  │
│                                                        ▼                   │  │
│                                          ┌─────────────────────────────┐  │  │
│                                          │     Storage (Shared)         │  │  │
│                                          │                             │  │  │
│                                          │  ┌─────────┐  ┌─────────┐  │  │  │
│                                          │  │msgStore │  │storageDB│  │  │  │
│                                          │  │(files)  │  │(bbolt)  │  │  │  │
│                                          │  └─────────┘  └─────────┘  │  │  │
│                                          └─────────────────────────────┘  │  │
│                                                                              │  │
└──────────────────────────────────────────────────────────────────────────────┘

  Response to sender:
  250 OK: Message accepted for delivery
  550 Requested action not taken (rejected by filter)
```

### 4.2 User Authentication Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Authentication Flow                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Email Client / Webmail                                                      │
│       │                                                                      │
│       │  1. POST /api/v1/auth/login                                        │
│       │     {email, password}                                                │
│       │                                                                     │
│       ▼                                                                     │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      HTTP API Layer                                  │   │
│  │                                                                      │   │
│  │   Rate Limit Check ──▶ Parse Request ──▶ Validate JSON              │   │
│  │                              │                                      │   │
│  │                              ▼                                      │   │
│  │                    ┌─────────────────┐                              │   │
│  │                    │  Get Account    │                              │   │
│  │                    │  from database  │                              │   │
│  │                    └────────┬────────┘                              │   │
│  │                             │                                        │   │
│  │                             ▼                                        │   │
│  │                    ┌─────────────────┐                              │   │
│  │                    │ Compare Password │                              │   │
│  │                    │ (bcrypt)        │                              │   │
│  │                    └────────┬────────┘                              │   │
│  │                             │                                        │   │
│  │                    ┌────────▼────────┐                            │   │
│  │                    │  Check TOTP      │                            │   │
│  │                    │  (if enabled)    │                            │   │
│  │                    └────────┬────────┘                              │   │
│  │                             │                                        │   │
│  └─────────────────────────────┼────────────────────────────────────────┘   │
│                                │                                             │
│                                ▼                                             │
│                    ┌───────────────────────┐                               │
│                    │   Generate JWT        │                               │
│                    │                       │                               │
│                    │  {                    │                               │
│                    │    sub: email,        │                               │
│                    │    admin: isAdmin,    │                               │
│                    │    exp: +24h          │                               │
│                    │  }                    │                               │
│                    └───────────┬───────────┘                               │
│                                │                                             │
│                                ▼                                             │
│                    ┌───────────────────────┐                               │
│                    │  Return Token          │                               │
│                    │  {token, expiresIn}    │                               │
│                    └───────────────────────┘                               │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘

  Subsequent requests include:
  Authorization: Bearer <token>
```

### 4.3 Outbound Email Flow (Queue)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      Outbound Email Flow (Queue)                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  User sends email via:                                                      │
│  - SMTP Submission (port 587)                                              │
│  - HTTP API (/api/v1/mail/send)                                            │
│       │                                                                      │
│       ▼                                                                      │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     Message Processing                                │   │
│  │                                                                      │   │
│  │  1. Validate recipient(s)                                            │   │
│  │  2. Generate Message-ID                                            │   │
│  │  3. Add Date, From headers                                         │   │
│  │  4. Queue message                                                   │   │
│  │                                                                      │   │
│  └──────────────────────────────────┬───────────────────────────────────┘   │
│                                     │                                        │
│                                     ▼                                        │
│                        ┌────────────────────────┐                           │
│                        │     Queue Manager       │                           │
│                        │                        │                           │
│                        │  • In-memory queue     │                           │
│                        │  • Persistent backup   │                           │
│                        │  • Retry logic         │                           │
│                        │  • Max retries: 5      │                           │
│                        └────────────┬───────────┘                           │
│                                     │                                        │
│                         ┌────────────┴────────────┐                          │
│                         │                         │                          │
│                         ▼                         ▼                          │
│              ┌──────────────────┐    ┌──────────────────┐                 │
│              │    MX Lookup      │    │  SPF/DKIM Sign   │                 │
│              │  (DNS)            │    │                  │                 │
│              └────────┬──────────┘    └────────┬─────────┘                 │
│                       │                        │                            │
│                       └────────────┬───────────┘                            │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                            │
│                         │  Deliver to Remote   │                            │
│                         │  SMTP Server         │                            │
│                         │                      │                            │
│                         │  Port 25 / MX        │                            │
│                         │  TLS if available    │                            │
│                         └─────────────────────┘                            │
│                                                                              │
│  Retry States:                                                               │
│  • QUEUE → RETRY (wait 15min) → RETRY → ... → DROP                          │
│  • Success → DONE                                                            │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Storage Architecture

### 5.1 Database Schema (bbolt)

**Buckets:**

| Bucket | Key Pattern | Value | Description |
|--------|------------|-------|-------------|
| `accounts` | `{domain}/{localPart}` | JSON | Account data |
| `domains` | `{domainName}` | JSON | Domain config |
| `aliases` | `{domain}/{localPart}` | String | Alias targets |
| `msgs:{user}:{mailbox}` | UID (uint32) | JSON | Message metadata |
| `mailbox:{user}:{mailbox}` | - | JSON | Mailbox metadata (uidnext, uidvalidity) |
| `queue` | `{messageID}` | JSON | Queue entries |
| `filters` | `{user}` | JSON | Sieve scripts |
| `vacation` | `{user}` | JSON | Auto-reply config |
| `push_subs` | `{endpoint}` | JSON | WebPush subscriptions |

**Accounts Database (accounts.db):**
```
Key: "example.com/alice"
Value: {
  "email": "alice@example.com",
  "localPart": "alice",
  "domain": "example.com",
  "passwordHash": "$2a$10$...",
  "isActive": true,
  "isAdmin": false,
  "quotaLimit": 1073741824,  // 1GB
  "quotaUsed": 536870912,     // 512MB used
  "forwardTo": "",
  "forwardKeepCopy": true,
  "totpEnabled": false,
  "totpSecret": "",
  "createdAt": "2024-01-01T00:00:00Z",
  "lastLoginAt": "2024-01-15T12:30:00Z"
}
```

### 5.2 Maildir++ Structure

```
./data/
├── mail/
│   ├── mail.db                    # bbolt metadata database
│   └── messages/
│       └── {user}@{domain}/
│           ├── INBOX/
│           │   ├── new/           # New messages (unseen)
│           │   ├── cur/           # Current messages (seen)
│           │   └── tmp/            # Temporary files
│           ├── Sent/
│           │   ├── new/
│           │   ├── cur/
│           │   └── tmp/
│           ├── Drafts/
│           ├── Trash/
│           └── Junk/
├── queue/                         # Outbound queue backup
├── umailserver.db                 # Accounts database (bbolt)
└── umailserver.pid                # PID file
```

### 5.3 Message File Format

**Filename:** `{hash}:2,{flags}`
- `hash`: SHA256 hash of message content
- `2`: Maildir version
- `flags`: R (read), S (seen), D (draft), T (trashed), F (flagged)

**File Content:** Raw email (RFC 5322 format)

Example:
```
From: sender@example.com
To: user@localhost
Subject: Hello
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <abc123@example.com>

Hello World!
```

---

## 6. Protocol Stack

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Protocol Stack                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                         Application Layer                              │   │
│  │                                                                      │   │
│  │   SMTP (5321)     IMAP4rev1 (3501)    POP3 (1939)    HTTP/1.1    │   │
│  │   ESMTP           IMAP4rev2 (9051)                     REST API    │   │
│  │   + Extensions    + Extensions                            JSON        │   │
│  │                                                                      │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                        Security Layer                                 │   │
│  │                                                                      │   │
│  │   TLS 1.2/1.3        SASL (PLAIN, LOGIN, SCRAM)     OAuth 2.0      │   │
│  │   (RFC 8314)         (RFC 4422)                      JWT           │   │
│  │                                                                      │   │
│  │   SMTP STARTTLS      IMAP AUTH                 HTTP Bearer        │   │
│  │   (RFC 3207)         (RFC 4954)                                   │   │
│  │                                                                      │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                       Authentication Layer                             │   │
│  │                                                                      │   │
│  │   SPF (7208)     DKIM (6376)      DMARC (7489)      ARC (8617)     │   │
│  │   Sender         Cryptographic   Policy         Auth Chain          │   │
│  │   Policy         Signature       Alignment      Preservation        │   │
│  │                                                                      │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                         Transport Layer                               │   │
│  │                                                                      │   │
│  │   TCP            TLS               UDP                               │   │
│  │   Port 25        Implicit TLS      (for mDNS only)                   │   │
│  │   Port 587       Port 465                                                │   │
│  │   Port 993       Port 995                                               │   │
│  │   Port 143                                                                  │   │
│  │   Port 110                                                                  │   │
│  │   Port 8080                                                                │   │
│  │                                                                      │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 7. Security Architecture

### 7.1 Authentication Mechanisms

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Authentication Architecture                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                        SMTP AUTH (Submission)                          │  │
│  │                                                                        │  │
│  │   ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐         │  │
│  │   │ PLAIN   │    │ LOGIN   │    │ SCRAM-  │    │  OAUTH  │         │  │
│  │   │ (RFC    │    │ (Legacy)│    │ SHA-256 │    │ BEARER  │         │  │
│  │   │ 4616)   │    │         │    │ (RFC    │    │ (RFC    │         │  │
│  │   │         │    │         │    │  7677)  │    │  7628)  │         │  │
│  │   └─────────┘    └─────────┘    └─────────┘    └─────────┘         │  │
│  │                                                                        │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                          IMAP/POP3 AUTH                                 │  │
│  │                                                                        │  │
│  │   Same mechanisms as SMTP AUTH plus:                                   │  │
│  │   • IMAP AUTHENTICATE command (RFC 4954)                              │  │
│  │   • POP3 AUTH command (RFC 5034)                                      │  │
│  │   • TOTP 2FA support                                                  │  │
│  │                                                                        │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                            JWT Auth                                    │  │
│  │                                                                        │  │
│  │   HS256 Signed Tokens                                                  │  │
│  │   • sub: email address                                                 │  │
│  │   • admin: boolean                                                     │  │
│  │   • exp: expiration (24h default)                                     │  │
│  │   • iat: issued at                                                    │  │
│  │                                                                        │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.2 Email Authentication

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Email Authentication Flow                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Incoming Email from external@example.com to user@localhost               │
│                              │                                                │
│                              ▼                                                │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │                          SPF Check                                   │  │
│  │                                                                       │  │
│  │   DNS Query: spf1.example.com                                        │  │
│  │   Result: PASS / FAIL / SOFTFAIL / NEUTRAL                          │  │
│  │                                                                       │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                              │                                                │
│                              ▼                                                │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │                          DKIM Verify                                   │  │
│  │                                                                       │  │
│  │   Extract: d=example.com; s=selector                                 │  │
│  │   DNS Query: selector._domainkey.example.com                          │  │
│  │   Verify: RSA signature using public key                              │  │
│  │   Result: PASS / FAIL                                                │  │
│  │                                                                       │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                              │                                                │
│                              ▼                                                │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │                          DMARC Check                                   │  │
│  │                                                                       │  │
│  │   DNS Query: _dmarc.example.com                                       │  │
│  │   Policy: none / quarantine / reject                                  │  │
│  │   Alignment: strict / relaxed                                         │  │
│  │   Result: PASS / FAIL                                                │  │
│  │                                                                       │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                              │                                                │
│                              ▼                                                │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │                      Spam Scoring                                     │  │
│  │                                                                       │  │
│  │   • Bayesian classifier (spam/ham)                                   │  │
│  │   • RBL checks (DNSBL/DNSWL)                                        │  │
│  │   • Heuristic rules                                                 │  │
│  │   • Score threshold: configurable                                    │  │
│  │                                                                       │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                              │                                                │
│                              ▼                                                │
│                    ┌─────────────────────┐                                 │
│                    │   Delivery Decision  │                                 │
│                    │                      │                                 │
│                    │  Score < junk: → INBOX│                                 │
│                    │  Score ≥ junk: → Junk│                                 │
│                    │  Score ≥ reject: →  │                                 │
│                    │              reject   │                                 │
│                    └─────────────────────┘                                 │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 8. RFC Compliance Matrix

### 8.1 Core Protocols

| RFC | Title | Status | Implementation |
|-----|-------|--------|----------------|
| RFC 5321 | SMTP | ✅ Full | `internal/smtp/` |
| RFC 6409 | Message Submission | ✅ Full | Port 587 |
| RFC 3207 | STARTTLS | ✅ Full | TLS upgrade |
| RFC 4954 | SMTP AUTH | ✅ Full | PLAIN, LOGIN, SCRAM |
| RFC 1939 | POP3 | ✅ Full | `internal/pop3/` |
| RFC 2449 | POP3 CAPA | ✅ Full | Extension framework |
| RFC 5034 | POP3 SASL | ✅ Full | Same as SMTP |
| RFC 8314 | Implicit TLS | ✅ Full | Ports 465, 993, 995 |
| RFC 3501 | IMAP4rev1 | ✅ Full | `internal/imap/` |
| RFC 9051 | IMAP4rev2 | ✅ Full | Via ENABLE extension |

### 8.2 Authentication & Security

| RFC | Title | Status | Implementation |
|-----|-------|--------|----------------|
| RFC 7208 | SPF | ✅ Full | `internal/auth/spf.go` |
| RFC 6376 | DKIM | ✅ Full | `internal/auth/dkim.go` |
| RFC 7489 | DMARC | ✅ Full | `internal/auth/dmarc.go` |
| RFC 8617 | ARC | ✅ Full | `internal/auth/arc.go` |
| RFC 7671 | DANE | ✅ Full | `internal/auth/dane.go` |
| RFC 7672 | DANE for SMTP | ✅ Full | TLSA record verification |
| RFC 4422 | SASL | ✅ Full | PLAIN, LOGIN, SCRAM-SHA-256 |
| RFC 7616 | SCRAM-SHA-256 | ✅ Full | Modern SASL mechanism |

### 8.3 Anti-Spam & Filtering

| RFC | Title | Status | Implementation |
|-----|-------|--------|----------------|
| RFC 5228 | Sieve | ✅ Full | `internal/sieve/` |
| RFC 5804 | ManageSieve | ✅ Full | Port 4190 |
| RFC 5782 | DNSBL/DNSWL | ✅ Full | RBL checker |
| RFC 6647 | Greylisting | ✅ Full | Greylist stage |
| RFC 2505 | Anti-Spam BCP | ✅ Full | SMTP pipeline |

### 8.4 Message Format & Extensions

| RFC | Title | Status | Implementation |
|-----|-------|--------|----------------|
| RFC 5322 | Message Format | ✅ Full | Standard format |
| RFC 2045-2049 | MIME | ✅ Full | Standard support |
| RFC 3461 | DSN | ✅ Full | `internal/queue/` |
| RFC 3798 | MDN | ✅ Full | Read receipts |
| RFC 6154 | SPECIAL-USE | ✅ Full | \Sent, \Drafts, etc. |
| RFC 6851 | IMAP MOVE | ✅ Full | MOVE command |

### 8.5 Delivery & Notifications

| RFC | Title | Status | Implementation |
|-----|-------|--------|----------------|
| RFC 3461 | DSN (Delivery Status) | ✅ Full | Delivery receipts |
| RFC 3798 | MDN (Disposition) | ✅ Full | Read receipts |
| RFC 5229 | Sieve Variables | ✅ Full | Vacation auto-reply |
| RFC 5230 | Sieve Vacation | ✅ Full | Auto-responder |

### 8.6 Web & Auto-Configuration

| Feature | Standard | Status | Implementation |
|---------|----------|--------|----------------|
| Autoconfig | Mozilla | ✅ Full | `/.well-known/autoconfig/` |
| Autodiscover | Microsoft | ✅ Full | `/autodiscover/` |

---

## 9. Directory Structure

```
uMailServer/
├── cmd/
│   └── umailserver/
│       └── main.go                 # Entry point
├── internal/
│   ├── server/
│   │   └── server.go              # Main orchestrator
│   ├── smtp/
│   │   ├── server.go              # SMTP server
│   │   ├── session.go             # SMTP session
│   │   ├── pipeline.go            # Pipeline stages
│   │   ├── stages/                # Individual stages
│   │   │   ├── spf.go
│   │   │   ├── dkim.go
│   │   │   ├── dmarc.go
│   │   │   ├── greylist.go
│   │   │   ├── rbl.go
│   │   │   ├── heuristic.go
│   │   │   └── av.go
│   │   └── auth.go                # SMTP AUTH
│   ├── imap/
│   │   ├── server.go              # IMAP server
│   │   ├── session.go             # IMAP session
│   │   ├── mailstore.go           # BboltMailstore
│   │   ├── commands.go           # IMAP commands
│   │   └── responses.go           # IMAP responses
│   ├── pop3/
│   │   ├── server.go              # POP3 server
│   │   └── session.go             # POP3 session
│   ├── api/
│   │   ├── server.go              # HTTP API server
│   │   ├── mail.go                # Mail endpoints
│   │   ├── auth.go                # Auth endpoints
│   │   ├── handlers.go            # Route handlers
│   │   └── middleware.go          # Auth/rate-limit middleware
│   ├── auth/
│   │   ├── spf.go                 # SPF verification
│   │   ├── dkim.go                # DKIM verification
│   │   ├── dmarc.go               # DMARC evaluation
│   │   ├── arc.go                 # ARC chain verification
│   │   ├── dane.go                # DANE/TLSA
│   │   └── totp.go                # TOTP 2FA
│   ├── storage/
│   │   ├── database.go            # bbolt wrapper
│   │   ├── messagestore.go        # Maildir++ implementation
│   │   └── search.go              # TF-IDF search service
│   ├── db/
│   │   └── db.go                  # Accounts/domains database
│   ├── queue/
│   │   ├── manager.go             # Outbound queue
│   │   └── delivery.go            # Queue delivery
│   ├── sieve/
│   │   ├── manager.go             # Sieve script manager
│   │   ├── interpreter.go         # Sieve execution
│   │   └── managesieve.go        # ManageSieve protocol
│   ├── spam/
│   │   └── classifier.go          # Bayesian spam classifier
│   ├── av/
│   │   └── scanner.go            # ClamAV integration
│   ├── vacation/
│   │   └── vacation.go           # Auto-responder
│   ├── webhook/
│   │   └── manager.go             # Event webhooks
│   ├── search/
│   │   └── service.go             # Full-text search
│   ├── tls/
│   │   └── manager.go             # ACME/Let's Encrypt
│   ├── config/
│   │   └── config.go              # YAML config loading
│   ├── health/
│   │   └── monitor.go             # Health checks
│   ├── metrics/
│   │   └── prometheus.go          # Prometheus metrics
│   ├── websocket/
│   │   └── sse.go                # Server-Sent Events
│   ├── push/
│   │   └── push.go               # WebPush notifications
│   ├── mcp/
│   │   └── server.go             # Model Context Protocol
│   ├── caldav/
│   │   └── caldav.go             # CalDAV calendar
│   ├── carddav/
│   │   └── carddav.go            # CardDAV contacts
│   ├── jmap/
│   │   └── handlers.go           # JMAP protocol
│   └── ...
├── web/
│   ├── admin/                    # Admin panel React app
│   ├── account/                  # Account settings React app
│   └── ...
├── webmail/
│   └── src/
│       ├── contexts/              # React contexts
│       │   ├── AuthContext.tsx
│       │   ├── EmailContext.tsx
│       │   └── ...
│       ├── components/           # React components
│       ├── pages/                # React pages
│       └── utils/                # Utilities
├── files/
│   ├── SPECIFICATION.md          # Detailed specification
│   ├── IMPLEMENTATION.md          # Implementation details
│   └── TASKS.md                  # Task tracking
├── embed.go                      # Frontend embedding
├── go.mod
├── go.sum
├── umailserver.yaml             # Configuration
└── ARCHITECTURE.md              # This file
```

---

## 10. Key Design Patterns

### 10.1 Pipeline Pattern (SMTP)

```go
// Pipeline stages implement this interface
type Stage interface {
    Name() string
    Process(ctx *PipelineContext) error
}

// Pipeline processes messages through ordered stages
type Pipeline struct {
    stages []Stage
}

func (p *Pipeline) AddStage(s Stage) {
    p.stages = append(p.stages, s)
}

func (p *Pipeline) Process(ctx *PipelineContext) error {
    for _, stage := range p.stages {
        if err := stage.Process(ctx); err != nil {
            if err == stage.ErrReject {
                return err
            }
            // Continue to next stage or abort
        }
    }
    return nil
}
```

### 10.2 Handler Injection

```go
// Dependencies are injected into servers
type SMTPServer struct {
    authHandler    AuthHandler
    deliveryHandler DeliveryHandler
    authFunc       func(user, pass string) (bool, error)
}

func (s *SMTPServer) SetAuthHandler(h AuthHandler) {
    s.authHandler = h
}

func (s *SMTPServer) SetDeliveryHandler(f func(...) error) {
    s.deliveryHandler = f
}
```

### 10.3 Shared Storage Pattern

```go
// Single storage instance shared across components
type Server struct {
    msgStore  *storage.MessageStore  // Maildir++
    storageDB *storage.Database      // bbolt for metadata
    mailstore *imap.BboltMailstore   // IMAP backend
}

// All components use the same storage
func NewBboltMailstoreWithInterfaces(db *storage.Database, msgStore *storage.MessageStore) {
    return &BboltMailstore{db: db, msgStore: msgStore}
}
```

### 10.4 WaitGroup Worker Pool

```go
// Goroutines tracked via sync.WaitGroup
func (s *Server) Start() {
    for i := 0; i < 10; i++ {
        s.wg.Add(1)
        go s.indexWorker()
    }
}

func (s *Server) Stop() {
    s.cancel()        // Signal shutdown
    s.wg.Wait()       // Wait for workers
}
```

---

## 11. Implementation History

### Recent Commits (Chronological)

| Commit | Description |
|--------|-------------|
| `c000ce3` | fix(webmail): connect API to real storage |
| `2196142` | chore: add umailserver.yaml to .gitignore |
| `bacbbb2` | feat: add webmail login page and demo setup script |
| `3cde2de` | chore: update docs with RFC features and benchmark client |
| `72e2805` | docs: add production and development config examples |
| `54044f5` | docs: add RFC reference guide and UI screenshots |
| `33ca141` | feat: implement RFC compliance features and benchmark client |
| `4e09f48` | feat(webmail): add welcome banner for new users |
| `2cf9438` | feat(webmail): improve compose and settings pages |
| `cdaad65` | feat(webmail): add keyboard shortcuts and improved sidebar |
| `f2ef3ee` | feat(webmail): polish UI with view modes and better UX |
| `e9bf450` | feat(webmail): translate UI to English |

### Key Features Implemented

1. **Core Email Server**
   - SMTP (MX + Submission)
   - IMAP4rev1/rev2
   - POP3
   - Maildir++ storage

2. **Authentication**
   - SPF verification
   - DKIM signing/verification
   - DMARC policy enforcement
   - ARC chain preservation
   - DANE/TLSA support
   - TOTP 2FA

3. **Anti-Spam**
   - Bayesian spam classifier
   - RBL/DNSBL checking
   - Greylisting
   - Heuristic scoring

4. **Filtering**
   - Sieve mail filtering (RFC 5228)
   - ManageSieve protocol (RFC 5804)
   - Vacation auto-responder (RFC 5230)

5. **Modern Protocols**
   - JMAP (HTTP-based email)
   - CalDAV/Calendars

6. **Observability**
   - Prometheus metrics
   - OpenTelemetry distributed tracing
   - Health checks
   - Structured logging

---

## 12. Observability Architecture

### 12.1 Metrics (`internal/metrics/`)

**Prometheus-compatible metrics endpoint:**

```
┌─────────────────────────────────────────────────────────────────┐
│                      Metrics Architecture                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────────┐    │
│   │   SMTP      │    │    IMAP     │    │      HTTP       │    │
│   │  metrics    │    │   metrics   │    │    metrics      │    │
│   │             │    │             │    │                 │    │
│   │ Connections │    │ Connections │    │   Requests      │    │
│   │ Messages    │    │ Commands    │    │   Latency       │    │
│   │ Failures    │    │ Errors      │    │   Errors        │    │
│   └──────┬──────┘    └──────┬──────┘    └────────┬────────┘    │
│          │                  │                    │              │
│          └──────────────────┴────────────────────┘              │
│                             │                                   │
│                             ▼                                   │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                  SimpleMetrics                          │   │
│   │                                                         │   │
│   │   • SMTP counters (connections, messages, failures)     │   │
│   │   • IMAP counters (connections, commands)               │   │
│   │   • Delivery counters (success, failed)                 │   │
│   │   • Spam counters (detected, ham)                       │   │
│   │   • API request counter                                 │   │
│   │   • Cache metrics (SPF, DKIM, DMARC hit/miss)           │   │
│   │                                                         │   │
│   └─────────────────────────┬───────────────────────────────┘   │
│                             │                                   │
│                             ▼                                   │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │              HTTP Handler (/metrics)                    │   │
│   │                                                         │   │
│   │   GET /metrics → Prometheus format                     │   │
│   │   GET /health → Health status                          │   │
│   │   GET /stats  → JSON format                            │   │
│   │                                                         │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Cache Metrics:**

| Metric | Type | Description |
|--------|------|-------------|
| `umailserver_spf_cache_hits` | Counter | SPF cache hits |
| `umailserver_spf_cache_misses` | Counter | SPF cache misses |
| `umailserver_dkim_cache_hits` | Counter | DKIM cache hits |
| `umailserver_dkim_cache_misses` | Counter | DKIM cache misses |
| `umailserver_dmarc_cache_hits` | Counter | DMARC cache hits |
| `umailserver_dmarc_cache_misses` | Counter | DMARC cache misses |

### 12.2 Distributed Tracing (`internal/tracing/`)

**OpenTelemetry Integration:**

```
┌─────────────────────────────────────────────────────────────────┐
│                     Tracing Architecture                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                   Tracing Provider                      │   │
│   │                                                         │   │
│   │   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │   │
│   │   │   Exporter   │  │   Resource   │  │   Sampler    │ │   │
│   │   │              │  │              │  │              │ │   │
│   │   │ • OTLP       │  │ • Service    │  │ • TraceID    │ │   │
│   │   │ • Stdout     │  │   Name       │  │   Ratio      │ │   │
│   │   │ • Noop       │  │ • Attributes │  │ • Config     │ │   │
│   │   └──────────────┘  └──────────────┘  └──────────────┘ │   │
│   │                                                         │   │
│   └─────────────────────────┬───────────────────────────────┘   │
│                             │                                   │
│         ┌───────────────────┼───────────────────┐               │
│         │                   │                   │               │
│         ▼                   ▼                   ▼               │
│   ┌──────────┐       ┌──────────┐       ┌──────────┐          │
│   │   SMTP   │       │   IMAP   │       │   HTTP   │          │
│   │  Spans   │       │  Spans   │       │  Spans   │          │
│   │          │       │          │       │          │          │
│   │ Connection│      │  Login   │       │  Request │          │
│   │  Delivery │      │ Command  │       │  Handler │          │
│   │ Pipeline │       │  Search  │       │  Auth    │          │
│   └──────────┘       └──────────┘       └──────────┘          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Supported Exporters:**

| Exporter | Endpoint | Use Case |
|----------|----------|----------|
| `noop` | - | Development, disabled |
| `stdout` | stdout | Debugging |
| `otlp` | `localhost:4317` | Production (Jaeger, Tempo) |

**Configuration:**

```yaml
tracing:
  enabled: true
  service_name: "umailserver"
  exporter: "otlp"
  otlp_endpoint: "localhost:4317"
  environment: "production"
  sample_rate: 1.0
```

### 12.3 Cache Architecture (`internal/auth/`)

**DNS Result Caching:**

```
┌─────────────────────────────────────────────────────────────────┐
│                      Cache Architecture                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                     SPF Cache                            │   │
│   │                                                         │   │
│   │   Domain → SPF Record (TTL: 5 minutes)                 │   │
│   │   Max size: 10,000 entries                             │   │
│   │   Eviction: LRU + expired entries                      │   │
│   │                                                         │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                    DKIM Cache                            │   │
│   │                                                         │   │
│   │   selector._domainkey.domain → Public Key             │   │
│   │   TTL: 5 minutes                                        │   │
│   │   Supports RSA and Ed25519 keys                        │   │
│   │                                                         │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                    DMARC Cache                           │   │
│   │                                                         │   │
│   │   Domain → DMARC Policy (TTL: 5 minutes)               │   │
│   │   Negative caching for non-existent records            │   │
│   │                                                         │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Cache Benefits:**
- Reduced DNS query load
- Improved email processing latency
- Better resilience during DNS outages
- Configurable TTL per cache type
   - CardDAV/Contacts
   - WebPush notifications
   - MCP (AI integration)

6. **Webmail**
   - React-based webmail
   - Full-text search
   - Real-time updates (WebSocket/SSE)
   - Autoconfig/Autodiscover

### Storage Evolution

The storage architecture has evolved through several iterations:

1. **Separate Databases** (Original)
   - `database` (accounts) at `./data/umailserver.db`
   - `storageDB` at `./data/umailserver.db` (same file!)
   - `msgStore` at `./data/messages/`

2. **Path Mismatch Bug**
   - `storageDB` opened via `cfg.Database.Path`
   - IMAP created separate `mailstore` at `./data/mail/mail.db`
   - Messages stored but not visible to API

3. **Current: Unified Storage**
   - All use `./data/mail/mail.db` for bbolt metadata
   - All use `./data/mail/messages/` for message files
   - Shared via `NewBboltMailstoreWithInterfaces()`

---

## Appendix: Configuration Reference

### Core Configuration (umailserver.yaml)

```yaml
server:
  hostname: mail.example.com
  data_dir: ./data

database:
  path: ./data/umailserver.db

http:
  bind: 0.0.0.0
  port: 8080

smtp:
  inbound:
    bind: 0.0.0.0
    port: 25
  submission:
    bind: 0.0.0.0
    port: 587
  max_message_size: 10MB

imap:
  bind: 0.0.0.0
  port: 143

pop3:
  bind: 0.0.0.0
  port: 110

tls:
  acme:
    enabled: true
    email: admin@example.com
    domains: [mail.example.com]

security:
  jwt_secret: your-secret-here
  rate_limit:
    http_requests_per_minute: 100
```

---

*Document generated: 2026-04-07*
*uMailServer Version: 1.0.0*
