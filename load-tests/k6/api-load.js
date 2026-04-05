import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const authTime = new Trend('api_auth_time');
const errorRate = new Rate('api_error_rate');

// Test configuration
export const options = {
  stages: [
    { duration: '1m', target: 50 },   // Warm up
    { duration: '3m', target: 200 },  // Ramp up
    { duration: '10m', target: 500 }, // Peak load
    { duration: '3m', target: 200 },  // Ramp down
    { duration: '1m', target: 0 },    // Cool down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
    api_auth_time: ['p(95)<1000'],
    api_error_rate: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.API_URL || 'http://localhost:8080';
const API_USER = __ENV.API_USER || 'admin@example.com';
const API_PASS = __ENV.API_PASS || 'admin123';

let authToken = null;

export function setup() {
  // Login and get token
  const loginRes = http.post(`${BASE_URL}/api/auth/login`, JSON.stringify({
    email: API_USER,
    password: API_PASS,
  }), {
    headers: { 'Content-Type': 'application/json' },
  });

  check(loginRes, {
    'login successful': (r) => r.status === 200,
  });

  const token = loginRes.json('token');
  return { token };
}

export default function (data) {
  const token = data.token;
  const headers = {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json',
  };

  try {
    // Test 1: Get stats (admin endpoint)
    const statsStart = Date.now();
    const statsRes = http.get(`${BASE_URL}/api/v1/stats`, { headers });
    const statsDuration = Date.now() - statsStart;

    check(statsRes, {
      'stats endpoint responds': (r) => r.status === 200,
      'stats load time < 500ms': () => statsDuration < 500,
    });

    // Test 2: List domains
    const domainsRes = http.get(`${BASE_URL}/api/v1/domains`, { headers });
    check(domainsRes, {
      'domains endpoint responds': (r) => r.status === 200,
    });

    // Test 3: List accounts
    const accountsRes = http.get(`${BASE_URL}/api/v1/accounts`, { headers });
    check(accountsRes, {
      'accounts endpoint responds': (r) => r.status === 200,
    });

    // Test 4: Get queue status
    const queueRes = http.get(`${BASE_URL}/api/v1/queue`, { headers });
    check(queueRes, {
      'queue endpoint responds': (r) => r.status === 200,
    });

    // Test 5: Health check (no auth required)
    const healthRes = http.get(`${BASE_URL}/health`);
    check(healthRes, {
      'health endpoint responds': (r) => r.status === 200,
    });

    errorRate.add(0);
  } catch (e) {
    console.error(`API Error: ${e.message}`);
    errorRate.add(1);
  }

  sleep(0.5);
}

export function handleSummary(data) {
  return {
    'api-load-summary.json': JSON.stringify(data),
    stdout: `
API Load Test Summary:
======================
Total Requests: ${data.metrics.http_reqs?.count || 0}
Failed Requests: ${data.metrics.http_req_failed?.passes || 0}
Avg Response Time: ${data.metrics.http_req_duration?.avg?.toFixed(2) || 'N/A'}ms
P95 Response Time: ${data.metrics.http_req_duration?.['p(95)']?.toFixed(2) || 'N/A'}ms
P99 Response Time: ${data.metrics.http_req_duration?.['p(99)']?.toFixed(2) || 'N/A'}ms
Error Rate: ${((data.metrics.api_error_rate?.rate || 0) * 100).toFixed(2)}%
Reqs/Sec: ${(data.metrics.http_reqs?.count / (data.state.testRunDurationMs / 1000)).toFixed(2) || 'N/A'}
`,
  };
}
