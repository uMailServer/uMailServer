# uMailServer

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://golang.org)
[![CI](https://github.com/uMailServer/uMailServer/actions/workflows/ci.yml/badge.svg)](https://github.com/uMailServer/uMailServer/actions)
[![License](https://img.shields.io/badge/license-AGPL%203.0%2FCommercial-green.svg)](LICENSE)

**One binary. Complete email.**

A modern, secure, high-performance mail server written in Go. Replaces Postfix + Dovecot + SpamAssassin + OpenDKIM + Roundcube with a single static binary.

---

## Why uMailServer?

Setting up a self-hosted mail server in 2026 still requires installing, configuring, and maintaining **6-8 separate components**:

| Component | Traditional Stack | uMailServer |
|-----------|------------------|-------------|
| SMTP (outbound) | Postfix | ✅ Built-in |
| SMTP (inbound/MX) | Postfix or Exim | ✅ Built-in |
| IMAP/POP3 | Dovecot | ✅ Built-in |
| Spam filtering | SpamAssassin | ✅ Built-in |
| DKIM signing | OpenDKIM | ✅ Built-in |
| SPF/DMARC | OpenDMARC | ✅ Built-in |
| Webmail | Roundcube/RainLoop | ✅ Modern React |
| SSL certificates | certbot | ✅ Auto ACME |

**Result:** uMailServer replaces all of this with a single Go binary + embedded React webmail.

---

## Features

### Core Protocols

- **SMTP** - Full RFC 5321 compliant server
  - Inbound MX on port 25
  - Submission on port 587 (STARTTLS)
  - Implicit TLS on port 465
  - 8BITMIME, SMTPUTF8, CHUNKING, PIPELINING support

- **IMAP** - RFC 3501 + IMAP4rev2 (RFC 9051)
  - IDLE for real-time push notifications
  - CONDSTORE/QRESYNC for efficient sync
  - SEARCH, SORT, THREAD
  - COMPRESS (DEFLATE)

- **POP3** - RFC 1939 compliant (for legacy clients)

### Security Stack

- **DKIM** - Sign and verify DomainKeys Identified Mail (RSA-2048/Ed25519)
- **DMARC** - Domain-based Message Authentication with policy enforcement
- **SPF** - Sender Policy Framework validation
- **ARC** - Authenticated Received Chain for forwarding
- **DANE** - DNS-based Authentication of Named Entities (TLSA)
- **MTA-STS** - SMTP MTA Strict Transport Security policy
- **Auto TLS** - Built-in Let's Encrypt ACME client with auto-renewal

### Anti-Spam Engine

Multi-layer scoring system (0.0 = clean, 10.0 = definite spam):

| Layer | Weight | Description |
|-------|--------|-------------|
| **RBL/DNSBL** | 0–4.0 | Spamhaus ZEN, Barracuda, SpamCop |
| **SPF** | 0–2.0 | Fail = +2.0, softfail = +1.0 |
| **DKIM** | 0–2.0 | Fail = +1.5, missing = +0.5 |
| **DMARC** | 0–2.0 | Fail = +2.0 |
| **Heuristic** | 0–5.0 | Pattern matching rules |
| **Bayesian** | -3.0–5.0 | Per-user trained classifier |
| **Greylisting** | 0–1.0 | Temporary reject for unknown triplets |

**Thresholds:**
- Score < 3.0 → Inbox
- Score 3.0–6.0 → Junk folder
- Score 6.0–9.0 → Quarantine
- Score ≥ 9.0 → Reject at SMTP level

### Webmail UI

Modern React 19 SPA with Tailwind CSS v4 and shadcn/ui components.

**Features:**
- Gmail-inspired 3-panel layout (sidebar + list + reader)
- Conversation/thread view
- Full-text search with filters
- Rich text composer (TipTap editor)
- Keyboard shortcuts (Gmail compatible)
- Drag & drop support
- Mobile-responsive with swipe gestures
- Dark/light theme

**Admin Panel:**
- Domain management with DNS helper
- Account CRUD with quotas
- Queue monitoring and retry
- DKIM key rotation
- Real-time statistics dashboard
- IP blocklist management

### API & Integrations

- **REST API** - JWT-based authentication for admin operations
- **MCP Server** - Model Context Protocol for AI assistants
- **Prometheus** - Metrics export
- **Webhooks** - Event callbacks (new mail, bounce, spam)

---

## Quick Start

### Installation

#### Option 1: Automated Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/umailserver/umailserver/main/scripts/install.sh | sudo bash
```

This will:
- Download the latest release for your architecture
- Create `umail` user and required directories
- Install systemd service
- Generate default configuration

#### Option 2: Manual Binary Download

```bash
# Download pre-built binary (Linux AMD64)
curl -L -o umailserver https://github.com/umailserver/umailserver/releases/latest/download/umailserver-linux-amd64
chmod +x umailserver
sudo mv umailserver /usr/local/bin/

# Or download for your platform:
# umailserver-linux-arm64
# umailserver-darwin-amd64
# umailserver-darwin-arm64
# umailserver-windows-amd64.exe
```

#### Option 3: Build from Source

```bash
git clone https://github.com/umailserver/umailserver.git
cd umailserver
go build -o umailserver ./cmd/umailserver
```

### Quick Setup

```bash
# Interactive setup wizard
./umailserver quickstart admin@example.com

# This will:
# 1. Generate configuration file
# 2. Create admin account
# 3. Print required DNS records
# 4. Generate DKIM keys
```

### Run

```bash
# Run in foreground
./umailserver serve -config umailserver.yaml

# Run as daemon
./umailserver serve --daemon

# Check status
./umailserver status
```

### Docker

```bash
# Using Docker Hub
docker run -d \
  --name umailserver \
  --restart unless-stopped \
  -p 25:25 -p 587:587 -p 465:465 \
  -p 110:110 -p 995:995 \
  -p 143:143 -p 993:993 \
  -p 8080:8080 \
  -v $(pwd)/data:/data \
  ghcr.io/umailserver/umailserver:latest

# Using Docker Compose (recommended)
curl -fsSL https://raw.githubusercontent.com/umailserver/umailserver/main/docker-compose.yml -o docker-compose.yml
docker-compose up -d

# View logs
docker-compose logs -f
```

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      uMailServer Binary                      │
│                                                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │  SMTP    │  │  IMAP    │  │  HTTP    │  │  Admin API  │  │
│  │ :25,:587 │  │ :993,:143│  │ :443,:80 │  │  :8443      │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬──────┘  │
│       │              │             │                │         │
│  ┌────┴──────────────┴─────────────┴────────────────┴─────┐  │
│  │                    Message Pipeline                     │  │
│  │  receive → auth → spam → dkim → store → deliver/relay  │  │
│  └────────────────────────┬────────────────────────────────┘  │
│                           │                                   │
│  ┌────────────────────────┴────────────────────────────────┐  │
│  │                     Storage Layer                        │  │
│  │  Maildir (messages) + Embedded DB (queue, config, index) │  │
│  └─────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌──────────────────────────────────────────────────────────┐│
│  │  Embedded UI (React SPA served via embed.FS)            ││
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  ││
│  │  │  Webmail    │  │ Admin Panel │  │  Self-Service   │  ││
│  │  │ (React 19)  │  │ (React 19)  │  │    Portal       │  ││
│  │  └─────────────┘  └─────────────┘  └─────────────────┘  ││
│  └──────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

---

## Configuration

### Minimal Config

```yaml
server:
  hostname: mail.example.com
  data_dir: /var/lib/umailserver

tls:
  acme:
    enabled: true
    email: admin@example.com

smtp:
  inbound:
    port: 25
  submission:
    port: 587

imap:
  port: 993

http:
  port: 443
```

### Full Example

See [config/config.example.yaml](config/config.example.yaml) for complete configuration options.

---

## CLI Commands

```bash
# Service management
umailserver serve                    # Start server
umailserver status                   # Show server status
umailserver stop                     # Stop daemon

# Domain management
umailserver domain add example.com   # Add domain
umailserver domain list              # List domains
umailserver domain dns example.com   # Print DNS records

# Account management
umailserver account add john@example.com      # Create account
umailserver account password john@example.com # Reset password
umailserver account list example.com          # List accounts

# Queue management
umailserver queue list               # View outbound queue
umailserver queue retry <id>         # Retry failed delivery
umailserver queue flush              # Retry all

# Diagnostics
umailserver check dns example.com    # Verify DNS records
umailserver check tls example.com    # Test TLS configuration
umailserver check deliverability     # Full deliverability audit

# Backup
umailserver backup /path/to/backup
umailserver restore /path/to/backup
```

---

## API Usage

### Authentication

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@example.com", "password": "secret"}'
```

### Create Domain

```bash
curl -X POST http://localhost:8080/api/v1/domains \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name": "example.com", "max_accounts": 100}'
```

### Create Account

```bash
curl -X POST http://localhost:8080/api/v1/accounts \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "password": "secret"}'
```

---

## Testing

```bash
# Run all tests
make test

# Run with race detection
make test-race

# Generate coverage report
make coverage

# Run linter
make lint
```

---

## Storage Design

### Mailbox Format: Maildir++

- One directory per mailbox
- One file per message (crash-safe, no locking)
- Dovecot-compatible for easy migration

```
/var/lib/umailserver/
  domains/
    example.com/
      users/
        john/
          Maildir/
            new/
            cur/
            tmp/
            .Sent/
            .Drafts/
            .Junk/
            .Trash/
```

### Embedded Database

- Queue persistence
- Configuration store
- Full-text search index
- Bayesian training data
- Rate limit counters

---

## Security

- **Passwords:** Argon2id hashing
- **TLS:** Minimum 1.2, prefer 1.3, strong cipher suites
- **Auth:** TOTP 2FA support, app passwords
- **Brute-force:** Auto IP blocking after failed attempts
- **Webmail:** DOMPurify sanitization, CSP headers
- **Storage:** Maildir (atomic file operations)

See [SECURITY.md](SECURITY.md) for vulnerability reporting and hardening guide.

---

## Performance

| Metric | Target | Status |
|--------|--------|--------|
| SMTP throughput | 10,000 msg/min | ✅ |
| IMAP connections | 10,000 concurrent | ✅ |
| Memory (idle) | < 100MB | ✅ |
| Binary size | < 50MB | ✅ |
| Startup time | < 2 seconds | ✅ |

---

## Roadmap

### v1.0 (Current)
- ✅ SMTP, IMAP, POP3
- ✅ DKIM, SPF, DMARC, ARC, DANE, MTA-STS
- ✅ Bayesian spam filtering, greylisting, RBL
- ✅ React webmail + admin panel
- ✅ MCP server integration
- ✅ ACME/Let's Encrypt auto-TLS

### v2.0 (Planned)
- Multi-node clustering
- S3-compatible storage backend
- LDAP/Active Directory integration
- CalDAV/CardDAV support
- Antivirus (YARA-based)

### v3.0 (Planned)
- AI-powered spam detection
- Email summarization
- Natural language search
- Smart categorization

---

## Migration Support

| Source | Method |
|--------|--------|
| Postfix + Dovecot | Maildir direct copy |
| cPanel | Backup file parser |
| Gmail | MBOX import |
| Any IMAP | `umailserver migrate --source imap://old` |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

By contributing, you agree that your contributions will be dual-licensed under AGPL-3.0 and Commercial terms.

---

## License

This project is dual-licensed:

- **AGPL-3.0** for community/non-commercial use
- **Commercial license** available for enterprise

See [LICENSE](LICENSE) for details.

---

## Links

- Website: https://umailserver.com
- GitHub: https://github.com/umailserver/umailserver
---

## Acknowledgments

Inspired by modern mail servers like Postfix, Dovecot, mox, Maddy, and Stalwart. Built with excellent Go libraries from the open-source community.
