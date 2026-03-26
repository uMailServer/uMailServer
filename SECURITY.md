# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.0.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

Please report security vulnerabilities to security@umailserver.com.

Do NOT open public issues for security bugs.

## Security Features

- TLS 1.3 support
- Automatic certificate management
- DKIM signing and verification
- DMARC policy enforcement
- SPF validation
- ARC chain validation
- DANE TLS authentication
- MTA-STS policy compliance
- Rate limiting
- Greylisting
- Bayesian spam filtering
- RBL checking

## Hardening Guide

1. Use strong TLS certificates (4096-bit RSA or ECDSA)
2. Enable DNSSEC on your domain
3. Configure SPF, DKIM, and DMARC records
4. Enable DANE/TLSA records
5. Set up MTA-STS policy
6. Use fail2ban or similar for brute force protection
7. Monitor logs for suspicious activity
8. Keep the software updated
