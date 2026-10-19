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

`umailserver backup` copies the bbolt databases, so it **refuses to run while
the server is up** ("database ... is locked by another process"). Stop the
server first and start it again afterwards.

```bash
sudo systemctl stop umailserver
# Full backup: accounts database, mail store, queue, CalDAV/CardDAV, push keys,
# vacation state and DKIM keys. The YAML config file is NOT in the archive: keep
# /etc/umailserver/umailserver.yaml in your own config management or copy it too.
# (the argument is a DIRECTORY; a timestamped umailserver_backup_<TS>.tar.gz is created inside)
umailserver backup /path/to/backups --config /etc/umailserver/umailserver.yaml

# With encryption (prefer the env var; --password is visible in ps)
UMAILSERVER_BACKUP_PASSWORD="strong-password" umailserver backup /backup --config /etc/umailserver/umailserver.yaml

sudo systemctl start umailserver
```

### Scheduling Backups (cron)

```bash
# Daily backup at 2 AM (the server is stopped for the duration of the backup)
0 2 * * * systemctl stop umailserver && UMAILSERVER_BACKUP_PASSWORD="password" umailserver backup /backups --config /etc/umailserver/umailserver.yaml; systemctl start umailserver
```

### Restoring from Backup

```bash
# Stop server first
sudo systemctl stop umailserver

# Extract and verify the archive into restore_temp/ (a sibling of data_dir);
# restore never writes into the live data directory itself
umailserver restore /path/to/umailserver_backup_<TS>.tar.gz.enc --password "password" --config /etc/umailserver/umailserver.yaml

# Copy the restored files into the data directory (paths assume
# server.data_dir: /var/lib/umailserver; see DEPLOYMENT.md for details)
sudo cp -a /var/lib/restore_temp/database/. /var/lib/umailserver/
sudo cp -a /var/lib/restore_temp/messages/. /var/lib/umailserver/
sudo chown -R umailserver:umailserver /var/lib/umailserver

# Verify
umailserver db status

# Restart
sudo systemctl start umailserver
```

---

## Health Checks

### Endpoint Checks

```bash
# Liveness (is the process alive?) - metrics listener, loopback only
curl http://127.0.0.1:8080/healthz

# Readiness (ready to accept traffic? 503 while shutting down)
curl http://localhost:443/health/ready

# Full health report
curl http://localhost:443/health
```

### Metrics

```bash
# Prometheus metrics (metrics.port, default 8080, bound to 127.0.0.1)
curl http://127.0.0.1:8080/metrics

# API metrics endpoint
curl -H "Authorization: Bearer <token>" http://localhost:443/api/v1/metrics
```

### Log Inspection

```bash
# Recent errors
tail -100 /var/log/umailserver/umailserver.log | grep -i error

# Audit log
tail -100 /var/lib/umailserver/logs/audit.log | jq .

# Queue status
curl -H "Authorization: Bearer <token>" http://localhost:443/api/v1/queue
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
   UMAILSERVER_BACKUP_PASSWORD="password" umailserver backup /tmp/failover --config /etc/umailserver/umailserver.yaml
   # writes /tmp/failover/umailserver_backup_<TS>.tar.gz.enc
   ```
4. **Transfer backup** to new server:
   ```bash
   scp /tmp/failover/umailserver_backup_*.tar.gz.enc new-server:/tmp/
   ```
5. **Restore on new server**:
   ```bash
   umailserver restore /tmp/umailserver_backup_<TS>.tar.gz.enc --password "password" --config /etc/umailserver/umailserver.yaml
   # then copy restore_temp/database/. and restore_temp/messages/. into data_dir
   # (see "Restoring from Backup" above)
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
umailserver config check --config /etc/umailserver/umailserver.yaml

# Check ports are available
ss -tlnp | grep -E ':(25|465|587|143|993|995|443|8080)\b'
```

**Common causes**:
- Port already in use → stop other service or change port
- Config file missing/invalid → regenerate with `umailserver quickstart <email>` and validate with `umailserver config check`
- Permission denied on data directory → `chown -R umailserver:umailserver /var/lib/umailserver`

### Database Locked

**Symptom**: `database is locked` errors in logs

```bash
# bbolt uses an OS file lock (no .lock file to delete): find the process that
# holds the database open; the CLI (account/domain/queue/backup/migrate)
# cannot open it while the server runs
lsof /var/lib/umailserver/umailserver.db
umailserver status
```

### Outbound Queue Stuck

**Symptom**: Emails queue but don't deliver

```bash
# Check queue status
curl -H "Authorization: Bearer <token>" http://localhost:443/api/v1/queue

# View pending entries
umailserver queue list

# Re-queue all failed entries (or one: umailserver queue retry <id>)
umailserver queue flush
```

### Authentication Failures

**Symptom**: Users can't login

```bash
# Check audit log for login failures
grep login_failure /var/lib/umailserver/logs/audit.log

# Verify account exists
curl -H "Authorization: Bearer <token>" http://localhost:443/api/v1/accounts/user@example.com

# Reset password if needed
curl -X PUT -H "Authorization: Bearer <token>" \
  -d '{"password": "newpassword123"}' \
  http://localhost:443/api/v1/accounts/user@example.com
```

---

## Recovery Procedures

### Corrupt Message Store

```bash
# 1. Stop server
sudo systemctl stop umailserver

# 2. Backup current state
cp -r /var/lib/umailserver /var/lib/umailserver.broken

# 3. Check database state
umailserver db status

# 4. If the database is unusable, restore from backup
umailserver restore /path/to/umailserver_backup_<TS>.tar.gz.enc --password "password"

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
umailserver check tls mail.example.com

# ACME renews automatically; to use a custom certificate set
# tls.cert_file / tls.key_file in umailserver.yaml and restart
```

### Disk Full

```bash
# Check disk usage
df -h /var/lib/umailserver

# Find large files
du -ah /var/lib/umailserver | sort -rh | head -20

# Clean old backups
ls -lt /backups   # delete old umailserver_backup_*.tar.gz* files manually

# Clean logs
journalctl --vacuum-size=100M
find /var/log/umailserver -name "*.log.*" -delete

# Clean mail queue (failed messages)
umailserver queue list   # then: umailserver queue drop <id>
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
