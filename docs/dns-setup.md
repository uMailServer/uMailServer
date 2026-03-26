# DNS Setup Guide

Complete DNS configuration for uMailServer.

## Required Records

### 1. A Record (Required)

Points your mail server hostname to your server's IP address.

```
mail.example.com.    IN    A    192.0.2.1
```

Replace `192.0.2.1` with your server's actual IP address.

### 2. MX Record (Required)

Tells other mail servers where to deliver email for your domain.

```
example.com.    IN    MX    10    mail.example.com.
```

- Priority `10` is standard (lower number = higher priority)
- Multiple MX records with different priorities for failover

### 3. SPF Record (Required)

Specifies which servers are allowed to send email for your domain.

```
example.com.    IN    TXT    "v=spf1 mx a:mail.example.com -all"
```

Variants:
```
# Basic (mail server only)
v=spf1 mx -all

# Include third-party services (e.g., SendGrid)
v=spf1 mx include:sendgrid.net -all

# Multiple servers
v=spf1 mx a:mail.example.com a:backup.example.com -all
```

**Mechanisms:**
- `mx` - Allow mail servers listed in MX records
- `a:hostname` - Allow specific hostname
- `ip4:192.0.2.0/24` - Allow IP range
- `include:domain.com` - Include another domain's SPF
- `~all` - Soft fail (mark as suspicious)
- `-all` - Hard fail (reject)

### 4. DKIM Record (Required)

Cryptographic signature to verify email authenticity.

```
default._domainkey.example.com.    IN    TXT    "v=DKIM1; k=rsa; p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQC1TaNgLlSyQMNWVLNLvyY/neDgaL2oqQE8T5illKqCgDtFHc8eHVAU+nlcaGmrKmDMw9dbgiGk1ocgZ56NR4ycfUHwQhvQPMUZw0cveel/8EAGoi/UyPmqfcPibytH81NFtTMAxUeM4Op8A6iHkvAMj5qLf4YRNsTkKAKW3OkwPQIDAQAB"
```

Get your DKIM key:
```bash
# After running quickstart, view the public key
cat /var/lib/umailserver/dkim/example.com.private.pem.pub
```

### 5. DMARC Record (Required)

Specifies how to handle emails that fail SPF or DKIM checks.

```
_dmarc.example.com.    IN    TXT    "v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com; ruf=mailto:dmarc-forensic@example.com; adkim=r; aspf=r; pct=100"
```

**Policy options:**
- `p=none` - Monitor only, take no action
- `p=quarantine` - Send to spam/junk folder (recommended for transition)
- `p=reject` - Reject failed emails (recommended after testing)

**Parameters:**
- `rua` - Aggregate reports sent to this address
- `ruf` - Forensic/failure reports
- `adkim=r` - Relaxed DKIM alignment
- `adkim=s` - Strict DKIM alignment
- `aspf=r` - Relaxed SPF alignment
- `pct=100` - Apply to 100% of emails

### 6. PTR Record (Reverse DNS) - Highly Recommended

Maps your IP address back to your hostname. Set this at your hosting provider, not in DNS.

```
1.2.0.192.in-addr.arpa.    IN    PTR    mail.example.com.
```

## Optional Records

### 7. MTA-STS (Recommended)

Enforces TLS encryption for incoming email.

```
_mta-sts.example.com.    IN    TXT    "v=STSv1; id=20240101T000000;"
```

Create policy file at `https://mta-sts.example.com/.well-known/mta-sts.txt`:
```
version: STSv1
mode: enforce
max_age: 604800
mx: mail.example.com
mx: backup.example.com
```

### 8. TLS-RPT (Recommended)

Reports on TLS connection failures.

```
_smtp._tls.example.com.    IN    TXT    "v=TLSRPTv1; rua=mailto:tls-reports@example.com"
```

### 9. DANE/TLSA (Advanced)

Certificate pinning via DNSSEC.

```
_25._tcp.mail.example.com.    IN    TLSA    3 1 1 a3c7c18e83e9b7708c0c7b8c5d9e8f3a2b1c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1
```

Generate TLSA record:
```bash
dig +short mail.example.com | xargs -I {} openssl s_client -connect {}:25 -starttls smtp 2>/dev/null < /dev/null | openssl x509 -noout -pubkey | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1
```

### 10. Autoconfig (Thunderbird)

Helps Thunderbird auto-configure.

```
autoconfig.example.com.    IN    CNAME    mail.example.com.
```

### 11. Autodiscover (Outlook)

Helps Outlook auto-configure.

```
autodiscover.example.com.    IN    CNAME    mail.example.com.
```

## Complete DNS Zone Example

```
; A Records
mail.example.com.           IN    A       192.0.2.1

; MX Record
example.com.                IN    MX      10 mail.example.com.

; SPF Record
example.com.                IN    TXT     "v=spf1 mx a:mail.example.com -all"

; DKIM Record
default._domainkey.example.com.    IN    TXT    "v=DKIM1; k=rsa; p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQC..."

; DMARC Record
_dmarc.example.com.         IN    TXT     "v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com"

; MTA-STS
_mta-sts.example.com.       IN    TXT     "v=STSv1; id=20240101T000000;"

; TLS-RPT
_smtp._tls.example.com.     IN    TXT     "v=TLSRPTv1; rua=mailto:tls-reports@example.com"
```

## Verification

### Check all records with uMailServer CLI:

```bash
umailserver check dns example.com
```

### Manual verification with dig:

```bash
# MX Record
dig MX example.com

# SPF Record
dig TXT example.com | grep spf

# DKIM Record
dig TXT default._domainkey.example.com

# DMARC Record
dig TXT _dmarc.example.com

# Reverse DNS
dig -x 192.0.2.1
```

### Online validation tools:

- [MXToolbox](https://mxtoolbox.com/SuperTool.aspx)
- [Google Admin Toolbox](https://toolbox.googleapps.com/apps/checkmx/)
- [Mail Tester](https://www.mail-tester.com/)

## Troubleshooting

### "No MX record found"
- Ensure MX record is published and propagated
- Check with: `dig MX example.com +short`
- Allow up to 24 hours for DNS propagation

### "SPF check failed"
- Verify SPF syntax is correct
- Ensure all sending IPs/servers are included
- Test with: `dig TXT example.com +short`

### "DKIM signature invalid"
- Ensure DKIM public key matches private key
- Check key is properly formatted (no line breaks in DNS)
- Verify selector matches configuration

### "DMARC policy not applied"
- Ensure SPF or DKIM passes (at least one)
- Check DMARC record syntax
- Verify rua email address is valid
