# uMailServer

**One binary. Complete email.**

uMailServer is a self-hosted mail server written in Go that replaces Postfix + Dovecot + SpamAssassin + OpenDKIM + Roundcube with a single binary.

SMTP · IMAP · Spam filtering · DKIM/SPF/DMARC · Modern webmail · Admin panel · MCP server

---

## Features

- **Single Binary** - One Go binary (~50MB), one config file, no Docker Compose needed
- **Modern Webmail** - React 19 + Tailwind v4 + shadcn/ui, rivals Gmail in UX
- **Complete Protocol Support** - SMTP (inbound/outbound), IMAP4rev2, POP3
- **Built-in Security** - DKIM, SPF, DMARC, ARC, MTA-STS, DANE
- **Spam Protection** - Bayesian classifier, RBL/DNSBL, heuristic rules, greylisting
- **Auto TLS** - Built-in ACME client for Let's Encrypt with auto-renewal
- **MCP Server** - AI agents can send/read email via Model Context Protocol
- **Pure Go** - No CGO, static binary, runs on Linux/macOS/FreeBSD

---

## Quick Start

```bash
# Download and install
curl -sSL https://umailserver.com/install.sh | bash

# Quick setup - generates config, creates first account, prints DNS records
umailserver quickstart you@example.com

# Start the server
umailserver serve
```

Your mail server is now running on ports:
- `25` - SMTP (incoming)
- `587` - SMTP submission (STARTTLS)
- `465` - SMTP submission (TLS)
- `993` - IMAP (TLS)
- `443` - HTTPS (webmail)
- `8443` - Admin panel

---

## Building from Source

```bash
# Clone
git clone https://github.com/umailserver/umailserver.git
cd umailserver

# Build
make build

# Or build without UI
make build-go

# Run tests
make test

# Build for all platforms
make release
```

---

## Configuration

See `umailserver.yaml.example` for full configuration options.

Key files:
- `/etc/umailserver/umailserver.yaml` - Main configuration
- `/var/lib/umailserver/` - Mail storage and database
- `/var/lib/umailserver/domains/` - Maildir++ storage

---

## CLI Commands

```bashn# Domain management
umailserver domain add example.com
umailserver domain list
umailserver domain dns example.com    # Show required DNS records

# Account management
umailserver account add john@example.com
umailserver account password john@example.com
umailserver account list example.com

# Queue management
umailserver queue list
umailserver queue retry <message-id>
umailserver queue flush

# Diagnostics
umailserver check dns example.com
umailserver check tls example.com
umailserver check deliverability

# Backup/Restore
umailserver backup /path/to/backup
umailserver restore /path/to/backup
```

---

## DNS Records

For each domain, uMailServer requires these DNS records:

```
# MX record (points to your mail server)
example.com.    IN    MX    10    mail.example.com.

# A record for mail server
mail.example.com.    IN    A    YOUR_SERVER_IP

# SPF record (allows your server to send for this domain)
example.com.    IN    TXT    "v=spf1 mx ~all"

# DKIM record (auto-generated per-domain)
default._domainkey.example.com.    IN    TXT    "v=DKIM1; k=rsa; p=..."

# DMARC record
_dmarc.example.com.    IN    TXT    "v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com"

# MTA-STS record
_mta-sts.example.com.    IN    TXT    "v=STSv1; id=20240101T000000"
```

---

## MCP Integration

uMailServer includes a built-in MCP server for AI agent integration:

Available tools:
- `umailserver_send` - Send an email
- `umailserver_search` - Search emails
- `umailserver_read` - Read a specific email
- `umailserver_list` - List emails in a folder
- `umailserver_move` - Move email(s) to folder
- `umailserver_delete` - Delete email(s)
- `umailserver_folders` - List folders
- `umailserver_stats` - Server statistics

---

## Documentation

- [Quick Start Guide](docs/quickstart.md)
- [Configuration Reference](docs/configuration.md)
- [DNS Setup Guide](docs/dns-setup.md)
- [Migration Guide](docs/migration.md)
- [Troubleshooting](docs/troubleshooting.md)

---

## Performance

| Metric | Target |
|--------|--------|
| SMTP throughput | 10,000 messages/minute |
| IMAP connections | 10,000 concurrent |
| Webmail load | < 1 second (initial) |
| Binary size | < 50MB |
| Memory (idle) | < 100MB |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md)

---

## License

- **AGPL-3.0** for community use
- **Commercial license** available for enterprise

---

## Support

- GitHub Issues: [github.com/umailserver/umailserver/issues](https://github.com/umailserver/umailserver/issues)
- Documentation: [umailserver.com/docs](https://umailserver.com/docs)