import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const apiLatency = new Trend('stress_api_latency');
const errorRate = new Rate('stress_error_rate');
const throughput = new Counter('stress_requests');

// Test configuration - Stress Test
export const options = {
  stages: [
    { duration: '5m', target: 100 },   // Normal load
    { duration: '5m', target: 500 },   // High load
    { duration: '10m', target: 1000 }, // Peak load
    { duration: '10m', target: 1000 }, // Sustained peak
    { duration: '5m', target: 500 },   // Recovery
    { duration: '5m', target: 100 },   // Normal
    { duration: '2m', target: 0 },     // Cool down
  ],
  thresholds: {
    http_req_duration: ['p(95)<1000'],
    http_req_failed: ['rate<0.05'],
    stress_error_rate: ['rate<0.05'],
  },
  ext: {
    loadimpact: {
      distribution: {
        'amazon:us:ashburn': { loadZone: 'amazon:us:ashburn', percent: 50 },
        'amazon:de:frankfurt': { loadZone: 'amazon:de:frankfurt', percent: 50 },
      },
    },
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_USER = __ENV.API_USER || 'admin@example.com';
const API_PASS = __ENV.API_PASS || 'admin123';

// Test scenarios
const SCENARIOS = {
  AUTH: 'auth',
  MAIL: 'mail',
  ADMIN: 'admin',
  HEALTH: 'health',
};

let authToken = null;

export function setup() {
  // Authenticate and get token
  const loginRes = http.post(`${BASE_URL}/api/auth/login`, JSON.stringify({
    email: API_USER,
    password: API_PASS,
  }), {
    headers: { 'Content-Type': 'application/json' },
  });

  check(loginRes, {
    'setup login successful': (r) => r.status === 200,
  });

  const token = loginRes.json('token');

  return { token, startTime: Date.now() };
}

export default function (data) {
  const token = data.token;
  const headers = {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json',
  };

  // Randomly select scenario based on VU number
  const scenario = __VU % 4;

  try {
    switch (scenario) {
      case 0:
        runAuthScenario(headers);
        break;
      case 1:
        runMailScenario(headers);
        break;
      case 2:
        runAdminScenario(headers);
        break;
      case 3:
        runHealthScenario();
        break;
    }

    throughput.add(1);
    errorRate.add(0);
  } catch (e) {
    console.error(`Error: ${e.message}`);
    errorRate.add(1);
  }

  sleep(Math.random() * 2);
}

function runAuthScenario(headers) {
  group('Auth Operations', () => {
    // Refresh token
    const start = Date.now();
    const res = http.post(`${BASE_URL}/api/auth/refresh`, null, { headers });
    apiLatency.add(Date.now() - start);

    check(res, {
      'token refresh successful': (r) => r.status === 200,
    });
  });
}

function runMailScenario(headers) {
  group('Mail Operations', () => {
    // Get inbox stats
    const start1 = Date.now();
    const statsRes = http.get(`${BASE_URL}/api/v1/mail/stats`, { headers });
    apiLatency.add(Date.now() - start1);

    check(statsRes, {
      'mail stats retrieved': (r) => r.status === 200,
    });

    // List messages
    const start2 = Date.now();
    const listRes = http.get(`${BASE_URL}/api/v1/mail/inbox?page=1&limit=20`, { headers });
    apiLatency.add(Date.now() - start2);

    check(listRes, {
      'mail list retrieved': (r) => r.status === 200,
    });

    // Send test email (occasionally)
    if (Math.random() > 0.8) {
      const start3 = Date.now();
      const sendRes = http.post(`${BASE_URL}/api/v1/mail/send`, JSON.stringify({
        to: ['test@example.com'],
        subject: `Stress Test ${Date.now()}`,
        body: 'This is a stress test email',
      }), { headers });
      apiLatency.add(Date.now() - start3);

      check(sendRes, {
        'email sent': (r) => r.status === 200,
      });
    }
  });
}

function runAdminScenario(headers) {
  group('Admin Operations', () => {
    // Get dashboard stats
    const start1 = Date.now();
    const statsRes = http.get(`${BASE_URL}/api/v1/stats`, { headers });
    apiLatency.add(Date.now() - start1);

    check(statsRes, {
      'admin stats retrieved': (r) => r.status === 200,
    });

    // List domains
    const start2 = Date.now();
    const domainsRes = http.get(`${BASE_URL}/api/v1/domains`, { headers });
    apiLatency.add(Date.now() - start2);

    check(domainsRes, {
      'domains listed': (r) => r.status === 200,
    });

    // List accounts (occasionally)
    if (Math.random() > 0.7) {
      const start3 = Date.now();
      const accountsRes = http.get(`${BASE_URL}/api/v1/accounts`, { headers });
      apiLatency.add(Date.now() - start3);

      check(accountsRes, {
        'accounts listed': (r) => r.status === 200,
      });
    }

    // Check queue (occasionally)
    if (Math.random() > 0.8) {
      const start4 = Date.now();
      const queueRes = http.get(`${BASE_URL}/api/v1/queue`, { headers });
      apiLatency.add(Date.now() - start4);

      check(queueRes, {
        'queue retrieved': (r) => r.status === 200,
      });
    }
  });
}

function runHealthScenario() {
  group('Health Checks', () => {
    // Health endpoint
    const start1 = Date.now();
    const healthRes = http.get(`${BASE_URL}/health`);
    apiLatency.add(Date.now() - start1);

    check(healthRes, {
      'health check passed': (r) => r.status === 200,
    });

    // Liveness probe
    const start2 = Date.now();
    const liveRes = http.get(`${BASE_URL}/health/live`);
    apiLatency.add(Date.now() - start2);

    check(liveRes, {
      'liveness check passed': (r) => r.status === 200,
    });

    // Readiness probe
    const start3 = Date.now();
    const readyRes = http.get(`${BASE_URL}/health/ready`);
    apiLatency.add(Date.now() - start3);

    check(readyRes, {
      'readiness check passed': (r) => r.status === 200,
    });

    // Metrics endpoint
    const start4 = Date.now();
    const metricsRes = http.get(`${BASE_URL}/metrics`);
    apiLatency.add(Date.now() - start4);

    check(metricsRes, {
      'metrics retrieved': (r) => r.status === 200,
    });
  });
}

export function teardown(data) {
  const duration = (Date.now() - data.startTime) / 1000;
  console.log(`Stress test completed in ${duration}s`);
}

export function handleSummary(data) {
  const duration = data.state.testRunDurationMs / 1000;
  const rps = data.metrics.http_reqs?.count / duration;

  return {
    'stress-test-summary.json': JSON.stringify(data, null, 2),
    'stress-test-report.html': generateHTMLReport(data),
    stdout: `
╔════════════════════════════════════════════════════════════╗
║           STRESS TEST SUMMARY                              ║
╠════════════════════════════════════════════════════════════╣
║ Duration: ${(duration / 60).toFixed(2)} minutes
║ Virtual Users: ${data.metrics.vus_max?.value || 'N/A'}
║ Total Requests: ${data.metrics.http_reqs?.count || 0}
║ Requests/Sec: ${rps?.toFixed(2) || 'N/A'}
╠════════════════════════════════════════════════════════════╣
║ Response Times (ms):
║   Min: ${data.metrics.http_req_duration?.min?.toFixed(2) || 'N/A'}
║   Avg: ${data.metrics.http_req_duration?.avg?.toFixed(2) || 'N/A'}
║   Max: ${data.metrics.http_req_duration?.max?.toFixed(2) || 'N/A'}
║   P50: ${data.metrics.http_req_duration?.med?.toFixed(2) || 'N/A'}
║   P95: ${data.metrics.http_req_duration?.['p(95)']?.toFixed(2) || 'N/A'}
║   P99: ${data.metrics.http_req_duration?.['p(99)']?.toFixed(2) || 'N/A'}
╠════════════════════════════════════════════════════════════╣
║ Errors: ${data.metrics.http_req_failed?.passes || 0}
║ Error Rate: ${((data.metrics.http_req_failed?.rate || 0) * 100).toFixed(2)}%
╚════════════════════════════════════════════════════════════╝
`,
  };
}

function generateHTMLReport(data) {
  // Simple HTML report generation
  return `
<!DOCTYPE html>
<html>
<head>
    <title>uMailServer Stress Test Report</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #4CAF50; color: white; }
        .metric { font-weight: bold; }
    </style>
</head>
<body>
    <h1>uMailServer Stress Test Report</h1>
    <p>Generated: ${new Date().toISOString()}</p>
    <h2>Summary</h2>
    <table>
        <tr><td>Total Requests</td><td>${data.metrics.http_reqs?.count || 0}</td></tr>
        <tr><td>Failed Requests</td><td>${data.metrics.http_req_failed?.passes || 0}</td></tr>
        <tr><td>Avg Response Time</td><td>${data.metrics.http_req_duration?.avg?.toFixed(2) || 'N/A'} ms</td></tr>
        <tr><td>P95 Response Time</td><td>${data.metrics.http_req_duration?.['p(95)']?.toFixed(2) || 'N/A'} ms</td></tr>
    </table>
</body>
</html>
`;
}
