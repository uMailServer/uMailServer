# uMailServer

[![Go Version](https://img.shields.io/badge/go-1.23+-blue.svg)](https://golang.org)
[![CI](https://github.com/uMailServer/uMailServer/actions/workflows/ci.yml/badge.svg)](https://github.com/uMailServer/uMailServer/actions)
[![License](https://img.shields.io/badge/license-AGPL%203.0%2FCommercial-green.svg)](LICENSE)

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

This project is dual-licensed:

- **AGPL-3.0** for community/non-commercial use
- **Commercial license** available for enterprise

See [LICENSE](LICENSE) for details.
