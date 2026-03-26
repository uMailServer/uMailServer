# Quick Start Guide

Get uMailServer up and running in under 5 minutes.

## Prerequisites

- Linux server (Ubuntu 22.04+, Debian 12+, or RHEL 9+)
- Public IP address
- Domain name with DNS control
- Ports 25, 587, 465, 993, 443, 80 available

## Installation

### Option 1: Automated Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/umailserver/umailserver/main/scripts/install.sh | sudo bash
```

This will:
- Download the latest release
- Create `umailserver` user and directories
- Install systemd service
- Open firewall ports (if applicable)

### Option 2: Manual Binary

```bash
# Download for your platform
curl -L -o umailserver https://github.com/umailserver/umailserver/releases/latest/download/umailserver-linux-amd64
chmod +x umailserver
sudo mv umailserver /usr/local/bin/

# Create directories
sudo mkdir -p /var/lib/umailserver /etc/umailserver
sudo chown -R umailserver:umailserver /var/lib/umailserver
```

## Initial Setup

### 1. Run Quickstart Wizard

```bash
sudo umailserver quickstart admin@yourdomain.com
```

Enter your admin password when prompted. The wizard will:
- Generate DKIM keys
- Create admin account
- Print required DNS records
- Create configuration file

### 2. Configure DNS Records

Add these DNS records at your registrar:

```
# MX Record (required)
yourdomain.com.    IN    MX    10    mail.yourdomain.com.

# A Record (required)
mail.yourdomain.com.    IN    A    <YOUR_SERVER_IP>

# SPF Record (required)
yourdomain.com.    IN    TXT    "v=spf1 mx ~all"

# DKIM Record (required)
default._domainkey.yourdomain.com.    IN    TXT    "v=DKIM1; k=rsa; p=<DKIM_KEY_FROM_WIZARD>"

# DMARC Record (recommended)
_dmarc.yourdomain.com.    IN    TXT    "v=DMARC1; p=quarantine; rua=mailto:dmarc@yourdomain.com"
```

### 3. Start the Server

```bash
# Run in foreground (for testing)
sudo umailserver serve -config /etc/umailserver/umailserver.yaml

# Or start as systemd service
sudo systemctl enable --now umailserver
```

### 4. Verify Installation

```bash
# Check server status
umailserver status

# Test DNS configuration
umailserver check dns yourdomain.com

# Test TLS
umailserver check tls mail.yourdomain.com
```

## First Login

1. Access the admin panel: `https://<your-server-ip>:8443`
2. Login with: `admin@yourdomain.com` and your password
3. Create additional email accounts

## Test Email Flow

### Send a test email:
```bash
umailserver test send admin@yourdomain.com user@example.com "Test Subject"
```

### Configure Email Client

**IMAP Settings:**
- Server: `mail.yourdomain.com`
- Port: `993`
- Security: `SSL/TLS`
- Authentication: Normal password

**SMTP Settings:**
- Server: `mail.yourdomain.com`
- Port: `587`
- Security: `STARTTLS`
- Authentication: Normal password

## Next Steps

- [Configure additional domains](configuration.md#domains)
- [Set up spam filtering](configuration.md#spam-filtering)
- [Enable monitoring](configuration.md#monitoring)
- [Read the full configuration guide](configuration.md)
