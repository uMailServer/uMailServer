import smtp from 'k6/x/smtp';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const deliveryTime = new Trend('smtp_delivery_time');
const errorRate = new Rate('smtp_error_rate');

// Test configuration
export const options = {
  stages: [
    { duration: '2m', target: 10 },   // Ramp up to 10 VUs
    { duration: '5m', target: 50 },   // Ramp up to 50 VUs
    { duration: '10m', target: 100 }, // Stay at 100 VUs
    { duration: '5m', target: 50 },   // Ramp down
    { duration: '2m', target: 0 },    // Ramp down to 0
  ],
  thresholds: {
    smtp_delivery_time: ['p(95)<5000'], // 95% under 5s
    smtp_error_rate: ['rate<0.1'],      // Error rate < 10%
    http_req_duration: ['p(95)<1000'],  // 95% under 1s
  },
};

const SMTP_SERVER = __ENV.SMTP_SERVER || 'localhost:25';
const SMTP_USER = __ENV.SMTP_USER || 'test@example.com';
const SMTP_PASS = __ENV.SMTP_PASS || 'test123';

// Generate test email
function generateEmail(to, subject, size) {
  const body = 'This is a test email. '.repeat(size / 25);
  return {
    from: SMTP_USER,
    to: [to],
    subject: subject,
    body: body,
  };
}

export default function () {
  const start = Date.now();

  try {
    // Connect to SMTP server
    const client = smtp.connect(SMTP_SERVER);

    // Authenticate
    const auth = client.auth(SMTP_USER, SMTP_PASS);
    check(auth, {
      'auth successful': (r) => r === true,
    });

    // Send email
    const to = `test-${__VU}-${__ITER}@example.com`;
    const email = generateEmail(to, `Load Test ${Date.now()}`, 1000);

    const result = client.send(email);

    const duration = Date.now() - start;
    deliveryTime.add(duration);

    check(result, {
      'email sent successfully': (r) => r === true,
      'delivery time < 5s': () => duration < 5000,
    });

    client.close();
    errorRate.add(0);
  } catch (e) {
    console.error(`SMTP Error: ${e.message}`);
    errorRate.add(1);
  }

  sleep(1);
}

export function handleSummary(data) {
  return {
    'smtp-load-summary.json': JSON.stringify(data),
    stdout: `
SMTP Load Test Summary:
======================
Messages Sent: ${data.metrics.http_reqs?.count || 'N/A'}
Avg Delivery Time: ${data.metrics.smtp_delivery_time?.avg?.toFixed(2) || 'N/A'}ms
P95 Delivery Time: ${data.metrics.smtp_delivery_time?.['p(95)']?.toFixed(2) || 'N/A'}ms
Error Rate: ${((data.metrics.smtp_error_rate?.rate || 0) * 100).toFixed(2)}%
`,
  };
}
