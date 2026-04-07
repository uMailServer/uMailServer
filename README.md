# uMailServer

<p align="center">
  <img src="assets/umailserver.jpeg" alt="uMailServer" width="100%">
</p>

<p align="center">
  <strong>One binary. Complete email.</strong>
</p>

<p align="center">
  <a href="https://github.com/umailserver/umailserver/releases">
    <img src="https://img.shields.io/github/v/release/umailserver/umailserver?style=flat-square" alt="Release">
  </a>
  <a href="https://github.com/umailserver/umailserver/actions">
    <img src="https://img.shields.io/github/actions/workflow/status/umailserver/umailserver/ci.yml?branch=main&style=flat-square" alt="CI">
  </a>
  <a href="https://goreportcard.com/report/github.com/umailserver/umailserver">
    <img src="https://goreportcard.com/badge/github.com/umailserver/umailserver?style=flat-square" alt="Go Report Card">
  </a>
  <a href="https://codecov.io/gh/umailserver/umailserver">
    <img src="https://img.shields.io/codecov/c/github/umailserver/umailserver?style=flat-square" alt="Coverage">
  </a>
  <a href="https://opensource.org/licenses/MIT">
    <img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="License">
  </a>
</p>

---

> **Note:** This project is in active development. For production use, please report any issues found.


**uMailServer** is a modern, self-hosted email server written in Go. It provides everything you need to run a complete email infrastructure: SMTP, IMAP, webmail, admin panel, spam filtering, automatic TLS certificates, and more — all in a single binary.

## Features

- **Single Binary**: Everything embedded, no external dependencies
- **Modern Protocols**: SMTP, IMAP, POP3 with full TLS support
- **Automatic TLS**: Let's Encrypt integration with auto-renewal (ACME v2)
- **Modern TLS**: TLS 1.2 and TLS 1.3 support with configurable minimum version
- **Spam Protection**: SPF, DKIM, DMARC, ARC, RBL, Bayesian filtering, greylisting, heuristic analysis
- **Antivirus**: ClamAV integration for virus scanning
- **Server-side Mail Filtering**: Sieve (RFC 5228) with ManageSieve (RFC 5804)
- **Email Encryption**: S/MIME (RFC 8551) and OpenPGP (RFC 3156) support
- **Delivery Notifications**: DSN (RFC 3461) - Success, Failure, Delay
- **Read Receipts**: MDN (RFC 3798) - Message Disposition Notifications
- **Auto Configuration**: Mozilla Autoconfig & Microsoft Autodiscover
- **Webmail**: Modern React-based web interface with real-time updates
- **Admin Panel**: Manage domains, accounts, queues, certificates
- **MCP Server**: Model Context Protocol for AI assistants
- **Queue Management**: Reliable outbound delivery with retry logic and exponential backoff
- **Full-text Search**: TF-IDF based email search
- **Webhooks**: Event notifications for integrations
- **Rate Limiting**: Per-IP and per-user rate limits for SMTP, IMAP, and HTTP
- **Metrics**: Prometheus-compatible metrics endpoint
- **Authentication**: Native bcrypt password hashing, LDAP/Active Directory support, TOTP 2FA
- **CalDAV/CardDAV**: Calendar and contacts synchronization
- **JMAP**: Modern email API (HTTP-based)
- **Docker**: First-class container support

## Quick Start

### Installation

```bash
# Install with one command
curl -fsSL https://get.umailserver.com | sudo bash

# Or download from releases
wget https://github.com/umailserver/umailserver/releases/latest/download/umailserver-linux-amd64
chmod +x umailserver-linux-amd64
sudo mv umailserver-linux-amd64 /usr/local/bin/umailserver
```

### Quickstart Command

```bash
# Setup your first domain and admin account
sudo umailserver quickstart admin@example.com

# Start the server
sudo umailserver serve

# Or with custom config
sudo umailserver serve --config /etc/umailserver/umailserver.yaml
```

### Docker

```bash
# Run with Docker Compose
docker compose up -d

# Or run directly
docker run -d \
  --name umailserver \
  -p 25:25 \
  -p 587:587 \
  -p 465:465 \
  -p 143:143 \
  -p 993:993 \
  -p 995:995 \
  -p 4190:4190 \
  -p 8443:8443 \
  -p 9443:9443 \
  -v umail_data:/data \
  ghcr.io/umailserver/umailserver:latest
```

### Port Reference

| Port | Protocol | Description |
|------|----------|-------------|
| 25 | SMTP | Inbound mail (MX) |
| 587 | SMTP | Submission (STARTTLS) |
| 465 | SMTP | Submission (Implicit TLS) |
| 143 | IMAP | IMAP (STARTTLS) |
| 993 | IMAP | IMAP (Implicit TLS) |
| 995 | POP3 | POP3 (Implicit TLS) |
| 4190 | ManageSieve | Sieve script management |
| 8443 | HTTPS | Webmail and API |
| 9443 | HTTPS | Admin Panel |
| 3000 | HTTP | MCP Server (Model Context Protocol) |

## Configuration

Create `/etc/umailserver/umailserver.yaml`:

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
    enabled: true
    port: 25
  submission:
    enabled: true
    port: 587
    require_auth: true
    require_tls: true
  submission_tls:
    enabled: true
    port: 465

imap:
  enabled: true
  port: 993
  starttls_port: 143

http:
  enabled: true
  port: 8443

admin:
  enabled: true
  port: 9443
  bind: 127.0.0.1

spam:
  enabled: true
  reject_threshold: 9.0
  junk_threshold: 3.0
  greylisting:
    enabled: true
    delay: 5m

domains:
  - name: example.com
    max_accounts: 100
    max_mailbox_size: 5GB
```

See [umailserver.yaml.example](umailserver.yaml.example) for full configuration options.

## Webmail

Access the webmail at `https://mail.example.com:8443/`

**Keyboard shortcuts:**

| Key | Action |
|-----|--------|
| `c` | Compose |
| `r` | Reply |
| `a` | Reply all |
| `f` | Forward |
| `e` | Archive |
| `#` | Delete |
| `s` | Star |
| `?` | Help |

## Admin Panel

Access the admin panel at `https://127.0.0.1:9443/`

Features:
- Dashboard with real-time stats
- Domain management with DNS helper
- Account management
- Queue monitoring
- Rate limiting management
- Security settings

## CLI Commands

```bash
# Server commands
umailserver serve                          # Start server
umailserver serve --config /path/to/config.yaml

# Account management
umailserver account add user@example.com
umailserver account delete user@example.com
umailserver account list example.com

# Domain management
umailserver domain add example.com
umailserver domain delete example.com
umailserver domain list

# Queue management
umailserver queue list
umailserver queue retry <id>
umailserver queue flush

# Diagnostics
umailserver check dns example.com
umailserver check tls example.com
umailserver check deliverability example.com

# Testing
umailserver test send from@example.com to@example.com "Test Subject"

# Backup & restore
umailserver backup /backups/
umailserver restore /backups/backup-2024-01-01.tar.gz

# Migration
umailserver migrate --type imap --source imaps://old-server.com --username user@old.com --target user@new.com
umailserver migrate --type dovecot --source /var/mail --passwd-file /etc/dovecot/users
umailserver migrate --type mbox --source /path/to/mail/*.mbox

# Get version
umailserver version
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        uMailServer                           │
├─────────────────────────────────────────────────────────────┤
│  SMTP (25, 587, 465)  │  IMAP (143, 993)  │  HTTP (8443)     │
├───────────────────────┼───────────────────┼──────────────────┤
│  Message Pipeline     │  Mailstore        │  REST API        │
│  - SPF/DKIM/DMARC     │  - Maildir++      │  - Auth (JWT)    │
│  - ARC Validation     │  - bbolt          │  - WebSocket     │
│  - Greylisting        │  - Search Index   │  - MCP           │
│  - RBL Checks         │                   │                  │
│  - Bayesian Filter    │                   │                  │
│  - Sieve Rules        │                   │                  │
│  - AV Scan            │                   │                  │
├───────────────────────┴───────────────────┴──────────────────┤
│         Web UI (React + Vite) - Admin, Account, Webmail      │
│              (embedded via embed.FS)                         │
└─────────────────────────────────────────────────────────────┘
```

## Development

```bash
# Clone repository
git clone https://github.com/umailserver/umailserver.git
cd umailserver

# Setup development environment
make setup

# Run in development mode (hot reload)
make dev

# Run tests
make test

# Build for all platforms
make build-all

# Build Docker image
make docker
```

## Requirements

- Linux, macOS, or Windows
- Go 1.25+ (for building)
- Node.js 20+ (for UI development)
- Docker (optional)

## Ports

| Port | Protocol | Description |
|------|----------|-------------|
| 25   | TCP      | SMTP (inbound mail) |
| 587  | TCP      | SMTP Submission (STARTTLS) |
| 465  | TCP      | SMTP Submission (Implicit TLS) |
| 143  | TCP      | IMAP (STARTTLS) |
| 993  | TCP      | IMAP (Implicit TLS) |
| 995  | TCP      | POP3 (Implicit TLS) |
| 4190 | TCP      | ManageSieve |
| 8443 | TCP      | Webmail and API (HTTPS) |
| 9443 | TCP      | Admin Panel (HTTPS) |
| 3000 | TCP      | MCP Server (HTTP) |
| 9090 | TCP      | Prometheus Metrics (optional) |

## Documentation

Local documentation in `docs/`:
- [Architecture](docs/ARCHITECTURE.md)
- [Configuration Reference](docs/configuration.md)
- [DNS Setup Guide](docs/dns-setup.md)
- [Migration Guide](docs/migration.md)
- [Quick Start](docs/quickstart.md)
- [Troubleshooting](docs/troubleshooting.md)
- [API Reference](docs/api-reference.md)

Online documentation at [docs.umailserver.com](https://docs.umailserver.com)

## Contributing

We welcome contributions! Please see [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
  Made with ❤️ by the uMailServer team
</p>
