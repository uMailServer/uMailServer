# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-03-26

### Added
- Initial release of uMailServer
- SMTP server with STARTTLS, authentication, and delivery
- IMAP server with folders, flags, and SEARCH support
- POP3 server with RFC 1939 compliance
- REST API with JWT authentication for admin operations
- DKIM signing and verification
- DMARC validation
- SPF validation
- ARC sealing and validation
- DANE validation
- MTA-STS policy handling
- Bayesian spam filtering
- Greylisting for spam prevention
- RBL (Real-time Blackhole List) checking
- Automatic TLS certificate provisioning with Let's Encrypt
- Webmail interface (single-page application)
- Maildir storage backend
- Message queue with retry logic
- Comprehensive test suite
