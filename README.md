# uMailServer

<p align="center">
  <img src="https://raw.githubusercontent.com/umailserver/umailserver/main/assets/logo.png" alt="uMailServer" width="200">
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

**uMailServer** is a modern, self-hosted email server written in Go. It provides everything you need to run a complete email infrastructure: SMTP, IMAP, webmail, admin panel, spam filtering, automatic TLS certificates, and more — all in a single binary.

## Features

- **Single Binary**: Everything embedded, no external dependencies
- **Modern Protocols**: SMTP, IMAP, POP3 with full TLS support
- **Automatic TLS**: Let's Encrypt integration with auto-renewal
- **Spam Protection**: SPF, DKIM, DMARC, ARC, RBL, Bayesian filtering, greylisting
- **Webmail**: Modern React-based web interface with real-time updates
- **Admin Panel**: Manage domains, accounts, queues, certificates
- **MCP Server**: Model Context Protocol for AI assistants
- **Queue Management**: Reliable outbound delivery with retry logic
- **Full-text Search**: TF-IDF based email search
- **Webhooks**: Event notifications for integrations
- **Metrics**: Prometheus-compatible metrics endpoint
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
```

### Docker

```bash
# Run with Docker Compose
docker-compose up -d

# Or run directly
docker run -d \
  --name umailserver \
  -p 25:25 \
  -p 587:587 \
  -p 465:465 \
  -p 993:993 \
  -p 8443:8443 \
  -v umail_data:/data \
  ghcr.io/umailserver/umailserver:latest
```

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
    port: 25
  submission:
    port: 587
  submission_tls:
    port: 465

imap:
  port: 993
  tls: true

domains:
  - name: example.com
    max_accounts: 100
    max_mailbox_size: 5GB
```

See [umailserver.yaml.example](umailserver.yaml.example) for full configuration options.

## Webmail

Access the webmail at `https://mail.example.com/webmail/`

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

Access the admin panel at `https://mail.example.com/admin/`

Features:
- Dashboard with real-time stats
- Domain management with DNS helper
- Account management
- Queue monitoring
- Certificate management
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
umailserver check deliverability

# Backup & restore
umailserver backup /backups/
umailserver restore /backups/backup-2024-01-01.tar.gz

# Migration
umailserver migrate --source imap://old-server.com --user user@example.com
umailserver migrate --source dovecot --passwd /etc/dovecot/users
umailserver migrate --source mbox /path/to/mail.mbox

# Get version
umailserver version
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        uMailServer                           │
├─────────────────────────────────────────────────────────────┤
│  SMTP (25, 587, 465)  │  IMAP (993)  │  HTTP (8443)          │
├───────────────────────┼──────────────┼───────────────────────┤
│  Message Pipeline     │  Mailstore   │  REST API             │
│  - SPF/DKIM/DMARC     │  - Maildir++ │  - Auth (JWT)         │
│  - Spam filter        │  - bbolt     │  - WebSocket          │
│  - Queue              │  - Search    │  - MCP                │
├───────────────────────┴──────────────┴───────────────────────┤
│                    Web UI (React + Vite)                     │
│         (embedded via embed.FS, served by Go)               │
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
- Go 1.24+ (for building)
- Node.js 20+ (for UI development)
- Docker (optional)

## Ports

| Port | Protocol | Description |
|------|----------|-------------|
| 25   | TCP      | SMTP (inbound mail) |
| 587  | TCP      | SMTP Submission (STARTTLS) |
| 465  | TCP      | SMTP Submission (Implicit TLS) |
| 993  | TCP      | IMAPS |
| 8443 | TCP      | Admin API & Web UI |
| 9090 | TCP      | Prometheus (optional) |
| 3000 | TCP      | Grafana (optional) |

## Documentation

- [Quick Start Guide](https://docs.umailserver.com/quickstart)
- [Configuration Reference](https://docs.umailserver.com/configuration)
- [DNS Setup Guide](https://docs.umailserver.com/dns-setup)
- [Migration Guide](https://docs.umailserver.com/migration)
- [API Documentation](https://docs.umailserver.com/api)
- [MCP Integration](https://docs.umailserver.com/mcp)

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Security

For security issues, please email security@umailserver.com or see [SECURITY.md](SECURITY.md).

## License

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
  Made with ❤️ by the uMailServer team
</p>
