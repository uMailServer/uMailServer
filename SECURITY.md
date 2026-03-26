# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.x     | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability within uMailServer, please send an email to security@umailserver.com. All security vulnerabilities will be promptly addressed.

Please do not disclose security-related issues publicly until a fix has been released.

## Security Features

### Authentication

- **Password Hashing:** All passwords are hashed using Argon2id
- **JWT Tokens:** Short-lived access tokens with refresh token rotation
- **2FA Support:** TOTP-based two-factor authentication
- **App Passwords:** Separate passwords for IMAP/POP3 clients

### Transport Security

- **TLS 1.3:** Preferred, with TLS 1.2 minimum
- **Certificate Pinning:** DANE/TLSA records supported
- **MTA-STS:** Strict Transport Security for SMTP
- **Auto TLS:** Automatic Let's Encrypt certificate provisioning

### Email Security

- **DKIM:** RSA-2048 and Ed25519 signing
- **SPF:** Sender Policy Framework validation
- **DMARC:** Policy enforcement and reporting
- **ARC:** Authenticated Received Chain for forwarding
- **DANE:** DNS-based certificate validation

### Web Security

- **CSP:** Content Security Policy headers
- **DOMPurify:** HTML sanitization
- **CORS:** Properly configured for API
- **Rate Limiting:** Prevents brute-force attacks

### Anti-Spam

- **Bayesian Filtering:** Per-user training
- **RBL:** Real-time blocklist checks
- **Greylisting:** Temporary rejection of unknown triplets
- **Rate Limiting:** Connection and message rate limits

## Hardening Guide

### Operating System

1. Run uMailServer as non-root user
2. Use a dedicated system user with minimal permissions
3. Enable SELinux/AppArmor if available
4. Keep the OS and dependencies updated

### Network

1. Use a firewall to restrict access to management ports
2. Enable fail2ban for SSH and other services
3. Use a reverse proxy (nginx/traefik) for HTTP/HTTPS
4. Consider using a CDN for static assets

### Application

1. Enable 2FA for all admin accounts
2. Use strong passwords (min 16 characters)
3. Regularly rotate DKIM keys
4. Monitor logs for suspicious activity
5. Keep backups encrypted and offsite

### Database

1. Encrypt data at rest
2. Regular backups
3. Access limited to uMailServer process

## Security Checklist

- [ ] Strong admin password set
- [ ] 2FA enabled for admin accounts
- [ ] TLS certificates configured
- [ ] DKIM keys generated
- [ ] DNS records verified
- [ ] Firewall configured
- [ ] Log monitoring enabled
- [ ] Backup encryption enabled
- [ ] Regular security updates

## Known Issues

None at this time.

## Credits

Thanks to all security researchers who have responsibly disclosed vulnerabilities.
