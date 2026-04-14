# Performance Tuning Guide

This guide covers performance optimization for uMailServer in production environments.

## Table of Contents

- [Overview](#overview)
- [System Requirements](#system-requirements)
- [Configuration Tuning](#configuration-tuning)
- [Database Optimization](#database-optimization)
- [Network Tuning](#network-tuning)
- [Storage Optimization](#storage-optimization)
- [Memory Management](#memory-management)
- [SMTP Performance](#smtp-performance)
- [IMAP Performance](#imap-performance)
- [Monitoring & Benchmarking](#monitoring--benchmarking)
- [Troubleshooting](#troubleshooting)

## Overview

uMailServer is designed for high-performance email delivery with configurable resource limits. This guide helps you optimize for your specific workload.

### Performance Targets

| Metric | Small (<1K users) | Medium (<10K users) | Large (>10K users) |
|--------|-------------------|---------------------|-------------------|
| SMTP msgs/sec | 100 | 1,000 | 5,000+ |
| IMAP connections | 100 | 1,000 | 10,000+ |
| Storage throughput | 10 MB/s | 100 MB/s | 500 MB/s+ |
| Latency (p99) | <100ms | <50ms | <20ms |

## System Requirements

### Hardware Recommendations

```
Small Deployment:
- CPU: 2 cores
- RAM: 4 GB
- Storage: 100 GB SSD
- Network: 1 Gbps

Medium Deployment:
- CPU: 4 cores
- RAM: 8 GB
- Storage: 500 GB SSD (RAID 10)
- Network: 10 Gbps

Large Deployment:
- CPU: 8+ cores
- RAM: 16+ GB
- Storage: 2+ TB NVMe SSD
- Network: 10+ Gbps
```

### Operating System Tuning

```bash
# /etc/sysctl.conf - Network tuning
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 65536
net.ipv4.tcp_max_syn_backlog = 65536
net.ipv4.tcp_fin_timeout = 10
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535

# File descriptor limits
# /etc/security/limits.conf
* soft nofile 1048576
* hard nofile 1048576
```

Apply changes:
```bash
sysctl -p
ulimit -n 1048576
```

## Configuration Tuning

### Connection Limits

```yaml
# umailserver.yaml
smtp:
  inbound:
    max_connections: 10000    # Increase for high volume
    max_recipients: 100       # RFC limit
  submission:
    max_connections: 10000

imap:
  max_connections: 10000      # Per server instance
  idle_timeout: "30m"         # Keep connections open
```

### Rate Limiting

```yaml
security:
  rate_limit:
    # Inbound (per IP)
    ip_per_minute: 100        # Increase for large user base
    ip_per_hour: 5000
    ip_per_day: 50000
    
    # Outbound (per user)
    user_per_minute: 120
    user_per_hour: 2000
    user_per_day: 10000
    
    # Global limits
    global_per_minute: 50000
    global_per_hour: 500000
```

### Queue Configuration

```yaml
queue:
  max_retries: 10
  retry_interval: "15m"
  max_queue_size: 100000     # Increase for high volume
  workers: 20                # Parallel delivery workers
```

## Database Optimization

### bbolt Tuning

```yaml
storage:
  database:
    path: "/var/lib/umailserver/mail.db"
    # bbolt options
    initial_mmap_size: 536870912  # 512 MB initial size
    page_size: 4096
    
database:
  path: "/var/lib/umailserver/umailserver.db"
```

### Database Maintenance

```bash
# Weekly compaction (schedule via cron)
0 2 * * 0 /usr/local/bin/umailserver compact-db

# Monitor database size
du -sh /var/lib/umailserver/*.db
```

## Network Tuning

### TCP Optimization

```yaml
server:
  # Increase buffer sizes
  read_buffer_size: 65536
  write_buffer_size: 65536

smtp:
  inbound:
    read_timeout: "5m"
    write_timeout: "5m"
```

### TLS Optimization

```yaml
tls:
  min_version: "1.3"          # Use TLS 1.3 for better performance
  cipher_suites:
    - "TLS_AES_128_GCM_SHA256"
    - "TLS_AES_256_GCM_SHA384"
    - "TLS_CHACHA20_POLY1305_SHA256"
```

## Storage Optimization

### Maildir Configuration

```yaml
storage:
  path: "/var/mail"
  sync: false                 # Disable fsync for performance
  shared_folders: true        # Enable shared mailboxes
```

### Filesystem Recommendations

```bash
# Mount options for ext4
/dev/sdb1 /var/mail ext4 noatime,nodiratime,nobarrier 0 2

# Mount options for XFS
/dev/sdb1 /var/mail xfs noatime,nodiratime,nobarrier 0 2

# Mount options for ZFS
zfs set atime=off mailpool/mail
zfs set sync=disabled mailpool/mail  # Use with UPS
```

### I/O Scheduler

```bash
# For SSD/NVMe
echo none > /sys/block/sda/queue/scheduler

# For RAID arrays
echo deadline > /sys/block/sda/queue/scheduler
```

## Memory Management

### Go Runtime Tuning

```bash
# Environment variables
export GOGC=100              # GC target percentage (default: 100)
export GOMAXPROCS=8          # Number of CPU cores
export GOMEMLIMIT=14GiB      # Soft memory limit (leave headroom)
```

### uMailServer Memory

```yaml
# Limit concurrent operations
server:
  max_memory_mb: 8192         # Soft memory limit
  
# Search indexing
search:
  max_index_memory: 1024      # MB for index cache
  index_workers: 10           # Parallel indexers
```

## SMTP Performance

### Pipeline Stages

Disable unnecessary stages for maximum throughput:

```yaml
spam:
  enabled: true
  greylisting:
    enabled: false            # Disable for high-volume inbound
  rbl_servers:                # Limit RBL checks
    - zen.spamhaus.org
    - b.barracudacentral.org

av:
  enabled: true
  timeout: "10s"              # Reduce timeout
  max_size: "25MB"            # Skip large messages
```

### Submission Performance

```yaml
smtp:
  submission:
    require_tls: true         # Always use TLS
    max_message_size: "50MB"
```

## IMAP Performance

### Connection Pooling

```yaml
imap:
  max_connections: 10000
  idle_timeout: "30m"         # Long-lived connections
  
# Enable IMAP COMPRESS extension
extensions:
  compress: true              # DEFLATE compression
```

### Mailbox Optimization

```yaml
# Auto-expunge settings
storage:
  auto_expunge: true          # Remove deleted messages
  expunge_interval: "1h"      # Hourly cleanup
```

## Monitoring & Benchmarking

### Key Metrics

Monitor these Prometheus metrics:

```promql
# SMTP throughput
rate(umailserver_smtp_messages_total[5m])

# Queue depth
umailserver_queue_size

# Database operations
rate(umailserver_db_operations_total[5m])

# Storage latency
histogram_quantile(0.99, 
  rate(umailserver_storage_duration_seconds_bucket[5m]))

# Memory usage
umailserver_memory_alloc_bytes
```

### Load Testing

```bash
# SMTP load test
swaks -t test@example.com -s localhost -p 587 \
  --auth-user user@example.com --auth-password secret \
  --num-messages 10000 --rate 100

# IMAP load test
imapsync --host1 localhost --user1 user@example.com \
  --password1 secret --host2 localhost --user2 user2@example.com \
  --password2 secret --usecache --nofoldersize

# HTTP API load test
wrk -t12 -c400 -d30s http://localhost:8080/api/v1/health
```

### Profiling

```bash
# Enable CPU profiling
export UMAILSERVER_PROFILE_CPU=true
export UMAILSERVER_PROFILE_MEM=true

# Generate profiles
curl http://localhost:8081/debug/pprof/profile > cpu.pprof
curl http://localhost:8081/debug/pprof/heap > heap.pprof

# Analyze
go tool pprof cpu.pprof
go tool pprof heap.pprof
```

## Troubleshooting

### High Memory Usage

```bash
# Check goroutine count
curl -s http://localhost:8081/debug/pprof/goroutine?debug=1

# Check heap profile
curl -s http://localhost:8081/debug/pprof/heap?debug=1 | head -20
```

### Slow SMTP Delivery

```bash
# Check queue depth
curl http://localhost:8080/api/v1/admin/queue | jq '.length'

# Check rate limits
curl http://localhost:8080/api/v1/admin/ratelimit | jq

# Review logs
journalctl -u umailserver -f | grep -i "slow\|timeout"
```

### Database Performance

```bash
# Monitor bbolt stats
curl http://localhost:8081/metrics | grep umailserver_db

# Check database size
ls -lh /var/lib/umailserver/*.db

# Compact database
umailserver compact-db
```

### Network Issues

```bash
# Check connection counts
ss -ant | grep :25 | wc -l
ss -ant | grep :993 | wc -l

# Check for SYN floods
netstat -tan | grep SYN_RECV | wc -l

# Monitor network throughput
iftop -i eth0
```

## Quick Reference

### Configuration Checklist

- [ ] Set appropriate connection limits
- [ ] Configure rate limiting
- [ ] Tune database settings
- [ ] Optimize storage mount options
- [ ] Set memory limits
- [ ] Enable monitoring
- [ ] Configure backups

### Performance Targets

| Check | Command | Target |
|-------|---------|--------|
| SMTP throughput | `swaks --rate 1000` | >1000 msg/s |
| IMAP latency | `imapsync --dry` | <50ms |
| Queue depth | API call | <1000 |
| Memory usage | `ps aux` | <80% |
| Disk I/O | `iostat -x 1` | <50% util |
| Network | `iftop` | <80% bandwidth |

## See Also

- [Production Readiness](./PRODUCTION_READINESS.md)
- [Distributed Tracing](./DISTRIBUTED_TRACING.md)
- [Monitoring Guide](./MONITORING.md)
- [Kubernetes Deployment](./KUBERNETES.md)
