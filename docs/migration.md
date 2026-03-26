# Migration Guide

Migrate from existing mail servers to uMailServer.

## Supported Sources

| Source | Method | Notes |
|--------|--------|-------|
| Dovecot | Maildir direct copy | Fast, preserves all metadata |
| Postfix + Dovecot | Maildir copy | Standard setup |
| cPanel | Backup restore | Use cPanel backup format |
| Gmail | MBOX export | Via Google Takeout |
| Any IMAP | IMAP sync | Universal migration |
| Thunderbird | MBOX import | Export folders |
| Outlook | PST export | Convert to MBOX first |

## Pre-Migration Checklist

- [ ] Backup existing mail server
- [ ] Note all email accounts and passwords
- [ ] List all mailboxes/folders per user
- [ ] Document forwarding rules and aliases
- [ ] Export contact lists
- [ ] Note filter/sieve rules

## Migration from Dovecot

### 1. Prepare uMailServer

```bash
# Set up uMailServer
umailserver quickstart admin@newdomain.com

# Create target domain
umailserver domain add example.com

# Create target accounts
umailserver account add user1@example.com
umailserver account add user2@example.com
```

### 2. Import Users (optional)

If you have a Dovecot passwd file:

```bash
umailserver migrate --type dovecot \
  --source /var/mail \
  --passwd-file /etc/dovecot/users
```

### 3. Copy Maildir

Direct filesystem copy (fastest):

```bash
# Copy maildir structure
rsync -avz --progress \
  /var/mail/example.com/ \
  /var/lib/umailserver/mail/example.com/

# Fix ownership
chown -R umailserver:umailserver /var/lib/umailserver/mail/
```

### 4. Verify Migration

```bash
# Check maildir structure
ls -la /var/lib/umailserver/mail/example.com/user1/Maildir/

# Count messages
find /var/lib/umailserver/mail/example.com/user1/Maildir/cur -type f | wc -l
```

## Migration via IMAP

Use when direct filesystem access isn't available.

### 1. Single Account Migration

```bash
umailserver migrate --type imap \
  --source imaps://oldmail.example.com \
  --username user@example.com \
  --password "oldpassword" \
  --target user@example.com
```

### 2. Bulk Migration Script

```bash
#!/bin/bash

# List of users to migrate
USERS=("user1@example.com" "user2@example.com" "user3@example.com")

for USER in "${USERS[@]}"; do
  echo "Migrating $USER..."
  umailserver migrate --type imap \
    --source imaps://oldmail.example.com \
    --username "$USER" \
    --password-file /path/to/passwords.txt \
    --target "$USER"
done
```

### 3. Dry Run

Test migration without making changes:

```bash
umailserver migrate --type imap \
  --source imaps://oldmail.example.com \
  --username user@example.com \
  --password "password" \
  --target user@example.com \
  --dry-run
```

## MBOX Import

Import from MBOX files (Thunderbird, Gmail export, etc.).

### 1. Export from Thunderbird

1. Install ImportExportTools NG addon
2. Right-click folder → "Save selected messages"
3. Choose "MBOX format (new)"

### 2. Import to uMailServer

```bash
# Import single MBOX file
umailserver migrate --type mbox \
  --source /path/to/Inbox.mbox \
  --target user@example.com

# Import multiple files
umailserver migrate --type mbox \
  --source "/path/to/mboxes/*.mbox" \
  --target user@example.com
```

### 3. Gmail Takeout Import

```bash
# Extract Google Takeout archive
tar -xzf takeout-20240101T000000Z-001.tgz

# Import all MBOX files
cd Takeout/Mail
for mbox in *.mbox; do
  folder=$(basename "$mbox" .mbox)
  echo "Importing $folder..."
  umailserver migrate --type mbox \
    --source "$mbox" \
    --target user@example.com \
    --folder "$folder"
done
```

## Gmail API Migration

For large Gmail migrations, use the Gmail API:

```bash
# Requires Gmail API credentials
umailserver migrate --type gmail \
  --source "gmail://user@gmail.com" \
  --credentials /path/to/credentials.json \
  --target user@example.com
```

## cPanel Migration

### 1. Create cPanel Backup

1. Log into cPanel
2. Go to "Backup" → "Download a Full Website Backup"
3. Download the backup file

### 2. Extract and Import

```bash
# Extract cPanel backup
tar -xzf backup-*.tar.gz

# Import accounts
umailserver migrate --type cpanel \
  --source extracted_backup/ \
  --domain example.com
```

## Migration with Imapsync

For complex migrations, use imapsync:

```bash
# Install imapsync
apt-get install imapsync

# Sync single account
imapsync \
  --host1 oldmail.example.com \
  --user1 user@example.com \
  --password1 "oldpass" \
  --host2 localhost \
  --user2 user@example.com \
  --password2 "newpass" \
  --ssl1 \
  --ssl2

# Parallel sync for multiple accounts
for user in user1 user2 user3; do
  imapsync \
    --host1 oldmail.example.com \
    --user1 "$user@example.com" \
    --password1 "oldpass" \
    --host2 localhost \
    --user2 "$user@example.com" \
    --password2 "newpass" &
done
wait
```

## Post-Migration Tasks

### 1. Update DNS

Point MX records to uMailServer:
```
example.com.    IN    MX    10    mail.newdomain.com.
```

### 2. Verify Migration

```bash
# Check DNS
umailserver check dns example.com

# Check TLS
umailserver check tls mail.example.com

# Send test email
umailserver test send user@example.com external@domain.com
```

### 3. Client Reconfiguration

Update email clients with new server settings:

**IMAP:**
- Server: mail.newdomain.com
- Port: 993 (SSL/TLS)

**SMTP:**
- Server: mail.newdomain.com
- Port: 587 (STARTTLS) or 465 (SSL/TLS)

### 4. Cutover Strategies

#### Big Bang (All at once)
1. Set old server to read-only
2. Final sync of all data
3. Update DNS MX records
4. Enable uMailServer

#### Parallel (Gradual)
1. Run both servers simultaneously
2. Use different subdomains
3. Migrate users in batches
4. Update MX priority to prefer new server

#### Shadow (Zero downtime)
1. Configure uMailServer as secondary MX
2. Replicate data continuously
3. Promote to primary MX
4. Decommission old server

## Troubleshooting

### "Authentication failed"
- Verify username/password
- Check if account exists in uMailServer
- Ensure target domain is created

### "Mailbox not found"
- Create target accounts first
- Verify domain exists
- Check permissions on maildir

### "Message too large"
- Increase `max_message_size` in config
- Default is 50MB

### "Connection timeout"
- Check firewall rules
- Verify old server is accessible
- Increase timeout in migration settings
