# uMailServer Runbook

> Operational procedures for running and maintaining uMailServer in production.

## Table of Contents

- [Startup & Shutdown](#startup--shutdown)
- [Backup & Restore](#backup--restore)
- [Health Checks](#health-checks)
- [Failover Procedures](#failover-procedures)
- [Common Failures](#common-failures)
- [Recovery Procedures](#recovery-procedures)
- [Escalation](#escalation)

---

## Startup & Shutdown

### Starting the Server

```bash
# Binary
umailserver serve

# Docker
docker-compose up -d

# Systemd
sudo systemctl start umailserver
```

### Stopping Gracefully

```bash
# Send SIGTERM for graceful shutdown (waits for active connections)
sudo kill -SIGTERM $(cat /var/lib/umailserver/umailserver.pid)

# Or via CLI
umailserver stop
```

### Forced Shutdown

```bash
# Only if graceful shutdown fails after 30s
sudo kill -SIGKILL $(cat /var/lib/umailserver/umailserver.pid)
```

---

## Backup & Restore

### Creating a Backup

```bash
# Full backup (config + database + messages)
umailserver backup create --path /path/to/backup.tar.gz.enc

# With encryption
umailserver backup create --path /backup/$(date +%Y%m%d).tar.gz.enc --password "strong-password"
```

### Scheduling Backups (cron)

```bash
# Daily backup at 2 AM
0 2 * * * umailserver backup create --path /backups/umailserver-$(date +\%Y\%m\%d).tar.gz.enc --password "password"
```

### Restoring from Backup

```bash
# Stop server first
sudo systemctl stop umailserver

# Restore
umailserver backup restore /path/to/backup.tar.gz.enc --password "password"

# Verify
umailserver diagnostics --check-storage

# Restart
sudo systemctl start umailserver
```

---

## Health Checks

### Endpoint Checks

```bash
# Liveness (is server alive?)
curl http://localhost:8080/health/live

# Readiness (is server ready to accept traffic?)
curl http://localhost:8080/health/ready

# Full health report
curl http://localhost:8080/health
```

### Metrics

```bash
# Prometheus metrics
curl http://localhost:9090/metrics

# API metrics endpoint
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/metrics
```

### Log Inspection

```bash
# Recent errors
tail -100 /var/log/umailserver/umailserver.log | grep -i error

# Audit log
tail -100 /var/lib/umailserver/logs/audit.log | jq .

# Queue status
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/queue
```

---

## Failover Procedures

### Primary Failover (Single Server → New Server)

1. **Prepare new server** with same version of uMailServer
2. **Stop primary server**:
   ```bash
   sudo systemctl stop umailserver
   ```
3. **Create fresh backup** on primary:
   ```bash
   umailserver backup create --path /tmp/failover-backup.tar.gz.enc --password "password"
   ```
4. **Transfer backup** to new server:
   ```bash
   scp /tmp/failover-backup.tar.gz.enc new-server:/tmp/
   ```
5. **Restore on new server**:
   ```bash
   umailserver backup restore /tmp/failover-backup.tar.gz.enc --password "password"
   ```
6. **Update DNS** MX records to point to new server
7. **Start new server**:
   ```bash
   sudo systemctl start umailserver
   ```

### DNS Failover (for high availability)

Configure secondary MX with lower priority:

```
mx1.example.com (priority 10) ← primary
mx2.example.com (priority 20) ← backup/secondary
```

When primary is down, mail queued by senders for retry.

### Rolling Restart (zero-downtime updates)

```bash
# For config changes or minor updates
# 1. Update config/binary
# 2. Send reload signal (graceful reload of config)
sudo kill -SIGHUP $(cat /var/lib/umailserver/umailserver.pid)

# For binary updates, do rolling restart:
# 1. Deploy new binary
# 2. For each active connection, server will naturally drain
# 3. Restart when connections are minimal
sudo systemctl restart umailserver
```

---

## Common Failures

### Server Won't Start

**Symptom**: `umailserver serve` exits immediately

```bash
# Check logs
tail -50 /var/log/umailserver/umailserver.log

# Verify config
umailserver setup --validate --config /etc/umailserver/umailserver.yaml

# Check ports are available
ss -tlnp | grep -E ':(25|465|587|143|993|110|995|8080)\b'
```

**Common causes**:
- Port already in use → stop other service or change port
- Config file missing/invalid → regenerate with `umailserver setup`
- Permission denied on data directory → `chown -R umailserver:umailserver /var/lib/umailserver`

### Database Locked

**Symptom**: `database is locked` errors in logs

```bash
# Check for stale lock files
ls -la /var/lib/umailserver/*.db

# Remove lock if server is not running
rm /var/lib/umailserver/umailserver.db.lock
```

### Outbound Queue Stuck

**Symptom**: Emails queue but don't deliver

```bash
# Check queue status
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/queue

# View pending entries
umailserver diagnostics --queue

# Force retry
umailserver queue retry --all
```

### Authentication Failures

**Symptom**: Users can't login

```bash
# Check audit log for login failures
grep login_failure /var/lib/umailserver/logs/audit.log

# Verify account exists
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/accounts/user@example.com

# Reset password if needed
curl -X PUT -H "Authorization: Bearer <token>" \
  -d '{"password": "newpassword123"}' \
  http://localhost:8080/api/v1/accounts/user@example.com
```

---

## Recovery Procedures

### Corrupt Message Store

```bash
# 1. Stop server
sudo systemctl stop umailserver

# 2. Backup current state
cp -r /var/lib/umailserver /var/lib/umailserver.broken

# 3. Run repair tool
umailserver diagnostics --repair

# 4. If repair fails, restore from backup
umailserver backup restore /path/to/backup.tar.gz.enc --password "password"

# 5. Restart
sudo systemctl start umailserver
```

### Lost JWT Secret

```bash
# Generate new secret
openssl rand -base64 32

# Update config
# Edit /etc/umailserver/umailserver.yaml:
# security:
#   jwt_secret: "new-secret-here"

# Restart server
sudo systemctl restart umailserver

# Note: All users must re-login after secret change
```

### TLS Certificate Issues

```bash
# Check certificate status
umailserver diagnostics --tls

# Force ACME renewal
umailserver tls renew

# Or import custom certificate
umailserver tls import --cert /path/to/cert.pem --key /path/to/key.pem
```

### Disk Full

```bash
# Check disk usage
df -h /var/lib/umailserver

# Find large files
du -ah /var/lib/umailserver | sort -rh | head -20

# Clean old backups
umailserver backup list
umailserver backup prune --keep 3

# Clean logs
journalctl --vacuum-size=100M
find /var/log/umailserver -name "*.log.*" -delete

# Clean mail queue (failed messages)
umailserver queue purge --status failed
```

---

## Escalation

### Severity Levels

| Level | Response Time | Examples |
|-------|---------------|----------|
| **P1** | 15 min | Total outage, mail loss, security breach |
| **P2** | 1 hour | Partial outage, queue stuck, authentication broken |
| **P3** | 4 hours | Non-critical bugs, performance degradation |
| **P4** | 24 hours | Minor issues, feature requests |

### Contacts

- **Primary Admin**: [admin@example.com]
- **On-Call**: [oncall@example.com]
- **Escalation**: [manager@example.com]

### Runbook History

| Date | Change | Author |
|------|--------|--------|
| 2026-04-10 | Initial runbook | uMailServer Team |
