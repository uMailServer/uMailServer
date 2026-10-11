# Migration Guide

Migrate from existing mail servers to uMailServer.

## Supported Sources

| Source | Method | Notes |
|--------|--------|-------|
| Dovecot | `migrate --type dovecot` | Imports the Maildir messages and (optionally) accounts from a passwd file |
| Postfix + Dovecot | `migrate --type dovecot` | Standard setup |
| Any IMAP (incl. Gmail with an app password) | `migrate --type imap` | Universal migration |
| Thunderbird, Gmail Takeout | `migrate --type mbox` | One file per destination account (see below) |
| cPanel, Outlook (PST) | Convert to Maildir or MBOX first | No dedicated importer |

`--type` accepts only `imap`, `dovecot` and `mbox`. The `migrate` command opens
the database directly, so **stop the server while it runs**.

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

### 3. Do not copy the Maildir by hand

uMailServer does not use a plain Maildir tree as its live store: messages are
content-addressed blobs under `mail/messages` with their metadata in
`mail/mail.db`. Copying a Maildir into the data directory (rsync) is **not**
picked up by IMAP/POP3. Use `migrate --type dovecot` as shown above, which
stores each message through the message store.

### 4. Verify Migration

Log in with an IMAP client (or `curl -u user:pass imaps://mail.example.com/INBOX`)
and compare message counts with the old server.

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
    --password "$(lookup_password "$USER")" \
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

`migrate --type mbox` takes the destination account from the **file name**:
`user@example.com.mbox` is imported into the account `user@example.com`
(the `.mbox` suffix is stripped). `--target` is ignored for MBOX imports and
there is no per-folder option, so rename each export to the account address
first:

```bash
cp /path/to/Inbox.mbox /tmp/user@example.com.mbox

# Import one file
umailserver migrate --type mbox --source /tmp/user@example.com.mbox

# Import several files (quote the glob; the CLI expands it)
umailserver migrate --type mbox --source "/tmp/*@example.com.mbox"
```

### 3. Gmail Takeout Import

Takeout produces one `.mbox` per label. Rename or merge the files per account
as above before importing. There is no Gmail API importer; to pull a live Gmail
mailbox use the IMAP importer with an app password, or `imapsync`.

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
