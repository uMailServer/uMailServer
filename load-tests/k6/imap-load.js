import imap from 'k6/x/imap';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const fetchTime = new Trend('imap_fetch_time');
const errorRate = new Rate('imap_error_rate');
const messagesFetched = new Counter('imap_messages_fetched');

// Test configuration
export const options = {
  stages: [
    { duration: '2m', target: 20 },   // Ramp up
    { duration: '5m', target: 100 },  // Peak load
    { duration: '5m', target: 100 },  // Sustained load
    { duration: '3m', target: 0 },    // Ramp down
  ],
  thresholds: {
    imap_fetch_time: ['p(95)<2000'],
    imap_error_rate: ['rate<0.05'],
  },
};

const IMAP_SERVER = __ENV.IMAP_SERVER || 'localhost:993';
const IMAP_USER = __ENV.IMAP_USER || 'test@example.com';
const IMAP_PASS = __ENV.IMAP_PASS || 'test123';

export default function () {
  const start = Date.now();

  try {
    // Connect and authenticate
    const client = imap.connect({
      host: IMAP_SERVER.split(':')[0],
      port: parseInt(IMAP_SERVER.split(':')[1]) || 993,
      tls: true,
    });

    client.login(IMAP_USER, IMAP_PASS);

    // Select inbox
    client.select('INBOX');

    // Fetch message list
    const messages = client.search(['ALL']);

    check(messages, {
      'search successful': (m) => Array.isArray(m),
    });

    // Fetch last 20 messages
    const toFetch = messages.slice(-20);

    for (const msgId of toFetch) {
      const msgStart = Date.now();
      const msg = client.fetch(msgId, ['FLAGS', 'BODY[HEADER.FIELDS (SUBJECT FROM DATE)]']);
      const msgDuration = Date.now() - msgStart;

      fetchTime.add(msgDuration);
      messagesFetched.add(1);

      check(msg, {
        'message fetched': (m) => m !== null,
      });
    }

    const duration = Date.now() - start;

    check(null, {
      'total fetch time < 10s': () => duration < 10000,
    });

    client.logout();
    client.close();

    errorRate.add(0);
  } catch (e) {
    console.error(`IMAP Error: ${e.message}`);
    errorRate.add(1);
  }

  sleep(2);
}

export function handleSummary(data) {
  return {
    'imap-load-summary.json': JSON.stringify(data),
    stdout: `
IMAP Load Test Summary:
======================
Active Connections: ${data.metrics.vus_max?.value || 'N/A'}
Messages Fetched: ${data.metrics.imap_messages_fetched?.count || 0}
Avg Fetch Time: ${data.metrics.imap_fetch_time?.avg?.toFixed(2) || 'N/A'}ms
P95 Fetch Time: ${data.metrics.imap_fetch_time?.['p(95)']?.toFixed(2) || 'N/A'}ms
Error Rate: ${((data.metrics.imap_error_rate?.rate || 0) * 100).toFixed(2)}%
`,
  };
}
