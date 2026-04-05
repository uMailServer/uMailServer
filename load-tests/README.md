# uMailServer Load Tests

This directory contains load and stress testing scripts for uMailServer using k6.

## Overview

These tests validate:
- **SMTP Performance**: Message throughput and delivery latency
- **IMAP Performance**: Connection handling and message fetch times
- **API Performance**: HTTP endpoint response times under load
- **WebSocket Performance**: Real-time connection handling
- **Stress Testing**: System behavior under extreme load

## Prerequisites

- [k6](https://k6.io/docs/get-started/installation/) installed
- Running uMailServer instance
- Test accounts configured

## Installation

```bash
# Install k6
# macOS
brew install k6

# Linux (Debian/Ubuntu)
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6

# Windows
choco install k6
# or
winget install k6

# Docker
docker pull grafana/k6
```

## Test Scripts

### 1. SMTP Load Test (`smtp-load.js`)

Tests email injection performance.

```bash
# Run with default settings
k6 run k6/smtp-load.js

# Run with custom settings
SMTP_SERVER=mail.example.com:25 \
SMTP_USER=test@example.com \
SMTP_PASS=test123 \
k6 run k6/smtp-load.js

# Run with specific VUs and duration
k6 run --vus 100 --duration 10m k6/smtp-load.js
```

**Metrics**:
- `smtp_delivery_time`: Time to send an email
- `smtp_error_rate`: Percentage of failed deliveries

### 2. IMAP Load Test (`imap-load.js`)

Tests IMAP connection and fetch performance.

```bash
IMAP_SERVER=mail.example.com:993 \
IMAP_USER=test@example.com \
IMAP_PASS=test123 \
k6 run k6/imap-load.js
```

**Metrics**:
- `imap_fetch_time`: Time to fetch messages
- `imap_messages_fetched`: Total messages retrieved

### 3. API Load Test (`api-load.js`)

Tests REST API endpoints.

```bash
API_URL=http://localhost:8080 \
API_USER=admin@example.com \
API_PASS=admin123 \
k6 run k6/api-load.js
```

**Endpoints tested**:
- POST /api/auth/login
- GET /api/v1/stats
- GET /api/v1/domains
- GET /api/v1/accounts
- GET /api/v1/queue
- GET /health

**Metrics**:
- `api_auth_time`: Authentication response time
- `http_req_duration`: All HTTP request durations

### 4. WebSocket Load Test (`websocket-load.js`)

Tests WebSocket real-time connections.

```bash
WS_URL=ws://localhost:8080/ws \
API_USER=test@example.com \
API_PASS=test123 \
k6 run k6/websocket-load.js
```

**Metrics**:
- `ws_connect_time`: Connection establishment time
- `ws_messages_received`: Total messages received
- `ws_error_rate`: Connection error rate

### 5. Stress Test (`stress-test.js`)

Comprehensive stress test combining all scenarios.

```bash
BASE_URL=http://localhost:8080 \
API_USER=admin@example.com \
API_PASS=admin123 \
k6 run k6/stress-test.js
```

**Load Pattern**:
- 5 min: Ramp to 100 VUs (normal load)
- 5 min: Ramp to 500 VUs (high load)
- 10 min: Ramp to 1000 VUs (peak load)
- 10 min: Sustain 1000 VUs
- 5 min: Ramp down to 500 VUs (recovery)
- 5 min: Ramp down to 100 VUs
- 2 min: Cool down

## Docker Compose Setup

Run full load testing environment with monitoring:

```bash
cd load-tests

# Start infrastructure
docker-compose up -d umailserver influxdb prometheus grafana

# Wait for services to be ready
sleep 30

# Run k6 tests
docker-compose up k6

# View results in Grafana: http://localhost:3000 (admin/admin)
# View Prometheus: http://localhost:9090

# Cleanup
docker-compose down -v
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SMTP_SERVER` | `localhost:25` | SMTP server address |
| `SMTP_USER` | `test@example.com` | SMTP username |
| `SMTP_PASS` | `test123` | SMTP password |
| `IMAP_SERVER` | `localhost:993` | IMAP server address |
| `IMAP_USER` | `test@example.com` | IMAP username |
| `IMAP_PASS` | `test123` | IMAP password |
| `API_URL` | `http://localhost:8080` | API base URL |
| `API_USER` | `admin@example.com` | API admin user |
| `API_PASS` | `admin123` | API admin password |
| `WS_URL` | `ws://localhost:8080/ws` | WebSocket URL |
| `BASE_URL` | `http://localhost:8080` | Base URL for stress tests |

## Test Results

### Example Output

```
SMTP Load Test Summary:
======================
Messages Sent: 15420
Avg Delivery Time: 234.56ms
P95 Delivery Time: 567.89ms
Error Rate: 0.12%

API Load Test Summary:
======================
Total Requests: 45678
Failed Requests: 12
Avg Response Time: 45.67ms
P95 Response Time: 123.45ms
P99 Response Time: 234.56ms
Error Rate: 0.03%
Reqs/Sec: 456.78
```

### Interpreting Results

**Good Performance**:
- SMTP Delivery P95 < 5s
- API Response P95 < 500ms
- IMAP Fetch P95 < 2s
- Error Rate < 1%

**Acceptable Performance**:
- SMTP Delivery P95 < 10s
- API Response P95 < 1000ms
- IMAP Fetch P95 < 5s
- Error Rate < 5%

**Needs Optimization**:
- SMTP Delivery P95 > 10s
- API Response P95 > 1000ms
- IMAP Fetch P95 > 5s
- Error Rate > 5%

## CI/CD Integration

### GitHub Actions

```yaml
name: Load Tests

on:
  schedule:
    - cron: '0 2 * * 0'  # Weekly on Sunday

jobs:
  load-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Setup k6
        run: |
          sudo gpg -k
          sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
          echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
          sudo apt-get update
          sudo apt-get install k6

      - name: Start test server
        run: docker-compose up -d umailserver

      - name: Wait for server
        run: sleep 30

      - name: Run API load test
        run: k6 run --out json=results.json load-tests/k6/api-load.js
        env:
          API_URL: http://localhost:8080

      - name: Upload results
        uses: actions/upload-artifact@v4
        with:
          name: load-test-results
          path: results.json
```

## Custom Test Scenarios

Create custom test scenarios by modifying the stage configuration:

```javascript
export const options = {
  stages: [
    { duration: '2m', target: 10 },   // Warm up
    { duration: '5m', target: 100 },  // Normal load
    { duration: '10m', target: 200 }, // Peak load
    { duration: '2m', target: 0 },    // Cool down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],  // 95% under 500ms
    http_req_failed: ['rate<0.01'],    // Error rate < 1%
  },
};
```

## Performance Tuning Tips

1. **Database**: Ensure bbolt is on fast storage (SSD/NVMe)
2. **Network**: Use connection pooling in tests
3. **Memory**: Monitor memory usage during tests
4. **CPU**: Watch for CPU throttling under load
5. **File Descriptors**: Increase ulimit for high concurrency

## Troubleshooting

### Connection Refused

```bash
# Check if server is running
curl http://localhost:8080/health

# Check firewall rules
sudo iptables -L | grep 8080
```

### High Error Rates

- Check server logs: `journalctl -u umailserver -f`
- Monitor resources: `htop`, `iostat`
- Check database locks

### k6 Errors

```bash
# Update k6
k6 version
docker pull grafana/k6:latest

# Check k6 extensions are installed
k6 version  # Should show extensions
```

## See Also

- [k6 Documentation](https://k6.io/docs/)
- [Performance Testing Best Practices](https://k6.io/docs/testing-guides/running-large-tests/)
- [Grafana Dashboards](https://grafana.com/grafana/dashboards/)
