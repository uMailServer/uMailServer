# Security Policy

## Supported Versions

| Version | Supported          |
|---------|------------------- |
| 1.x     | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting Vulnerabilities

If you discover a security vulnerability in uMailServer, please report it responsibly.

**Please do not open public issues for security vulnerabilities.**

Instead, please email: **security@umailserver.com**

Include the following information:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will:
1. Acknowledge receipt within 48 hours
2. Provide a timeline for a fix
3. Credit you in the security advisory (unless you prefer anonymity)

## Security Features

- bcrypt password hashing (library default cost 10; Argon2id verification is also supported)
- Automatic TLS with Let's Encrypt
- SPF, DKIM, DMARC verification
- Rate limiting and brute force protection (per-IP and per-account login lockouts on SMTP, IMAP, POP3, ManageSieve, CalDAV/CardDAV and the REST API; TOTP attempt limits)
- Outbound MTA-STS enforcement without plaintext fallback; DANE/TLSA lookups for outbound delivery are advisory only (TLSA data is trusted only when DNSSEC-authenticated, and the stock server installs no validating-resolver hook, so records are logged, not enforced)
- MCP server binds to loopback by default; a non-loopback bind is refused unless `mcp.auth_token` is set explicitly (with no token configured on loopback, a generated one is stored in `<data_dir>/mcp_auth_token`)
- Audit logs record the client IP from `X-Forwarded-For`/`X-Real-IP` only when the direct peer is listed in `http.trusted_proxies`
- The server refuses to open a database written by a newer binary (upgrade the binary rather than downgrading)
- Input validation and sanitization
- Sandboxed HTML email rendering
