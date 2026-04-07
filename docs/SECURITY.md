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

- bcrypt password hashing (cost 12)
- Automatic TLS with Let's Encrypt
- SPF, DKIM, DMARC verification
- Rate limiting and brute force protection
- Input validation and sanitization
- Sandboxed HTML email rendering
