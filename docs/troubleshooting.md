# Troubleshooting Guide

Common issues and their solutions.

## Installation Issues

### "permission denied" when running umailserver

```bash
# Make binary executable
chmod +x umailserver

# Or use sudo
sudo ./umailserver
```

### "port already in use" error

```bash
# Check what's using port 25
sudo lsof -i :25
sudo ss -tlnp | grep :25

# Stop conflicting service
sudo systemctl stop postfix
sudo systemctl stop exim4
sudo systemctl disable postfix

# Or configure uMailServer to use different ports
```

### "cannot bind to port" (privileged ports)

```bash
# Option 1: Run as root (not recommended)
sudo ./umailserver serve

# Option 2: Grant capabilities
cap setcap cap_net_bind_service=+ep ./umailserver

# Option 3: Use higher ports and reverse proxy
```

## SMTP Issues

### "Connection refused" on port 25

**Check:**
```bash
# Verify uMailServer is listening
sudo ss -tlnp | grep :25

# Check firewall
sudo iptables -L | grep 25
sudo ufw status | grep 25

# Check if port is open externally
nc -zv yourdomain.com 25
```

**Solutions:**
```bash
# Open firewall
sudo ufw allow 25/tcp
sudo ufw allow 587/tcp
sudo ufw allow 465/tcp

# Check SELinux
sudo setsebool -P httpd_can_sendmail 1
```

### "Relay access denied"

**Cause:** Trying to send without authentication on port 25

**Solutions:**
1. Use port 587 with STARTTLS + AUTH
2. Configure IP whitelist in config
3. Authenticate before sending

### "Message rejected: SPF check failed"

**Check:**
```bash
# Verify SPF record
dig TXT example.com +short
umailserver check dns example.com
```

**Solutions:**
- Add sender's IP to SPF record
- Check if sending from authorized server
- Review SPF record syntax

### "DKIM signature verification failed"

**Check:**
```bash
# Verify DKIM DNS record
dig TXT default._domainkey.example.com +short

# Check DKIM key file
ls -la /var/lib/umailserver/dkim/
cat /var/lib/umailserver/dkim/example.com.private.pem.pub
```

**Solutions:**
- Regenerate DKIM keys
- Update DNS record
- Verify selector matches config

### "TLS handshake failed"

**Check:**
```bash
# Test TLS
openssl s_client -connect mail.example.com:587 -starttls smtp
openssl s_client -connect mail.example.com:465

# Check certificate
openssl s_client -connect mail.example.com:587 -starttls smtp 2>/dev/null | openssl x509 -noout -text
```

**Solutions:**
- Ensure hostname matches certificate
- Check certificate hasn't expired
- Verify TLS version compatibility

## IMAP Issues

### "Authentication failed" in email client

**Check:**
```bash
# Test IMAP authentication manually
curl -v imaps://user:pass@mail.example.com/

# Check user exists
umailserver account list example.com
```

**Solutions:**
- Verify username format (full email)
- Reset password: `umailserver account password user@example.com`
- Check if account is active

### "Cannot connect to IMAP server"

**Check:**
```bash
# Verify IMAP is listening
sudo ss -tlnp | grep :993

# Test connection
openssl s_client -connect mail.example.com:993
```

**Solutions:**
- Open port 993 in firewall
- Check if IMAP is enabled in config
- Verify TLS configuration

### "Mailbox doesn't exist"

**Solutions:**
```bash
# Create mailbox manually
mkdir -p /var/lib/umailserver/mail/example.com/user/Maildir/{cur,new,tmp}
chown -R umailserver:umailserver /var/lib/umailserver/mail/
```

## Web/HTTP Issues

### "Cannot access webmail"

**Check:**
```bash
# Verify HTTP server is running
sudo ss -tlnp | grep :443

# Check logs
tail -f /var/log/umailserver.log

# Test locally
curl -k https://localhost:443/
```

**Solutions:**
- Open port 443/80 in firewall
- Check for port conflicts
- Verify TLS certificate

### "Admin panel 404"

**Check:**
```bash
# Verify admin panel is enabled in config
cat /etc/umailserver/umailserver.yaml | grep -A5 "^admin:"

# Check if port is listening
sudo ss -tlnp | grep :8443
```

### "CORS errors in browser"

**Solutions:**
```yaml
# Add to config
http:
  cors_origins:
    - "https://mail.example.com"
    - "https://admin.example.com"
```

## Queue Issues

### "Messages stuck in queue"

**Check:**
```bash
# View queue
umailserver queue list

# Check queue status
curl http://localhost:8080/api/v1/admin/queue
```

**Solutions:**
```bash
# Retry specific message
umailserver queue retry <message-id>

# Retry all failed
umailserver queue flush

# Check DNS for recipient domain
dig MX recipient-domain.com

# Check if destination is reachable
telnet recipient-mx.com 25
```

### "Too many retries"

**Check:**
```bash
# View message details
umailserver queue show <message-id>

# Check logs
grep "queue" /var/log/umailserver.log
```

**Common causes:**
- Recipient server down
- DNS resolution issues
- IP blacklisted
- TLS negotiation failure

## Storage Issues

### "Disk full"

**Check:**
```bash
# Check disk usage
df -h /var/lib/umailserver

# Check maildir size
du -sh /var/lib/umailserver/mail/*

# Find large mailboxes
find /var/lib/umailserver/mail -type d -name "Maildir" -exec du -sh {} \; | sort -hr | head -20
```

**Solutions:**
```bash
# Clean up old messages
find /var/lib/umailserver/mail -type f -mtime +30 -delete

# Enable auto-cleanup in config
storage:
  retention:
    junk: 30d
    trash: 30d
```

### "Permission denied on maildir"

**Solutions:**
```bash
# Fix ownership
sudo chown -R umailserver:umailserver /var/lib/umailserver/mail
sudo chmod -R 700 /var/lib/umailserver/mail

# Check selinux context
sudo restorecon -Rv /var/lib/umailserver/mail
```

## Performance Issues

### "High memory usage"

**Check:**
```bash
# Monitor memory
ps aux | grep umailserver
free -h

# Check goroutines
curl http://localhost:8080/debug/pprof/goroutine?debug=1
```

**Solutions:**
- Reduce `max_workers` in config
- Limit concurrent connections
- Enable connection timeouts

### "Slow IMAP response"

**Solutions:**
```bash
# Check for locked mailboxes
lsof /var/lib/umailserver/mail/

# Rebuild search index
umailserver index rebuild

# Check disk I/O
iostat -x 1
```

## DNS Issues

### "DNS lookup failed"

**Check:**
```bash
# Test DNS resolution
dig MX gmail.com
dig TXT example.com

# Check resolv.conf
cat /etc/resolv.conf

# Test with specific server
dig @8.8.8.8 MX example.com
```

**Solutions:**
- Check network connectivity
- Verify DNS server accessibility
- Check for DNS rate limiting

## Security Issues

### "IP blocked due to failed logins"

**Check:**
```bash
# View blocked IPs
umailserver security list-blocks

# Check security logs
grep "brute-force" /var/log/umailserver.log
```

**Solutions:**
```bash
# Unblock IP
umailserver security unblock <ip-address>

# Adjust thresholds in config
auth:
  max_login_attempts: 10
  lockout_duration: 15m
```

### "Certificate expired"

**Solutions:**
```bash
# Force certificate renewal
umailserver tls renew --force

# Check ACME logs
journalctl -u umailserver | grep acme
```

## Database Issues

### "Database locked"

**Solutions:**
```bash
# Check for stuck processes
lsof /var/lib/umailserver/umailserver.db

# Restart server
umailserver stop
umailserver start
```

### "Database corruption"

**Solutions:**
```bash
# Backup first
cp /var/lib/umailserver/umailserver.db /var/lib/umailserver/umailserver.db.bak

# Check integrity
umailserver db check

# Repair
umailserver db repair
```

## Logging & Debugging

### Enable debug logging

```yaml
server:
  log_level: debug
```

### View logs

```bash
# Follow logs
tail -f /var/log/umailserver.log

# Journal (systemd)
journalctl -u umailserver -f

# Filter by component
grep "smtp" /var/log/umailserver.log
grep "imap" /var/log/umailserver.log
grep "queue" /var/log/umailserver.log
```

### Get system status

```bash
# Full status
umailserver status --verbose

# Health check
curl http://localhost:8080/health

# Metrics
curl http://localhost:9090/metrics
```

## Getting Help

### Before reporting an issue:

1. Check this troubleshooting guide
2. Review logs for error messages
3. Try the diagnostic commands
4. Check [GitHub Issues](https://github.com/umailserver/umailserver/issues)

### Information to include in bug reports:

- uMailServer version: `umailserver version`
- OS and version
- Configuration (redact sensitive info)
- Relevant log entries
- Steps to reproduce

### Support channels:

- GitHub Issues: [github.com/umailserver/umailserver/issues](https://github.com/umailserver/umailserver/issues)
- Documentation: [docs.umailserver.com](https://docs.umailserver.com)
- Community Discord: [discord.gg/umailserver](https://discord.gg/umailserver)
