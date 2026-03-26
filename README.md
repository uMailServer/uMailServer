# uMailServer

A modern, secure, high-performance mail server written in Go.

## Features

- **SMTP** - RFC 5321 compliant with STARTTLS
- **IMAP** - RFC 3501 with folders and SEARCH
- **POP3** - RFC 1939 compliant retrieval
- **REST API** - JWT-based admin API
- **DKIM/DMARC/SPF** - Email authentication
- **ARC/DANE/MTA-STS** - Advanced security
- **Anti-spam** - Bayesian, greylisting, RBL
- **Auto TLS** - Let's Encrypt integration

## Quick Start

```bash
go build -o umailserver ./cmd/umailserver
./umailserver -config config.yaml
```

## Docker

```bash
docker build -t umailserver .
docker run -p 25:25 -p 587:587 -p 8080:8080 umailserver
```

## License

MIT License
