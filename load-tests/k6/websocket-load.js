import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const connectTime = new Trend('ws_connect_time');
const messageLatency = new Trend('ws_message_latency');
const errorRate = new Rate('ws_error_rate');
const messagesReceived = new Counter('ws_messages_received');
const messagesSent = new Counter('ws_messages_sent');

// Test configuration
export const options = {
  stages: [
    { duration: '2m', target: 10 },   // Ramp up to 10 concurrent connections
    { duration: '5m', target: 50 },   // Ramp up to 50 connections
    { duration: '10m', target: 100 }, // Sustained load at 100 connections
    { duration: '3m', target: 0 },    // Ramp down
  ],
  thresholds: {
    ws_connect_time: ['p(95)<1000'],
    ws_error_rate: ['rate<0.05'],
    ws_messages_received: ['count>1000'],
  },
};

const WS_URL = __ENV.WS_URL || 'ws://localhost:8080/ws';
const API_USER = __ENV.API_USER || 'test@example.com';
const API_PASS = __ENV.API_PASS || 'test123';

function getAuthToken() {
  // In real scenario, this would be an HTTP call to get JWT
  return 'test-token';
}

export default function () {
  const token = getAuthToken();
  const url = `${WS_URL}?token=${token}`;

  const start = Date.now();

  const res = ws.connect(url, null, function (socket) {
    const connectDuration = Date.now() - start;
    connectTime.add(connectDuration);

    socket.on('open', () => {
      console.log(`VU ${__VU}: Connected`);

      // Subscribe to inbox updates
      socket.send(JSON.stringify({
        action: 'subscribe',
        mailbox: 'INBOX',
      }));
      messagesSent.add(1);

      // Send ping every 30 seconds
      socket.setInterval(function () {
        socket.send(JSON.stringify({ action: 'ping' }));
        messagesSent.add(1);
      }, 30000);

      // Simulate user actions
      socket.setTimeout(function () {
        socket.send(JSON.stringify({
          action: 'list',
          mailbox: 'INBOX',
          page: 1,
          limit: 20,
        }));
        messagesSent.add(1);
      }, 2000);

      socket.setTimeout(function () {
        socket.send(JSON.stringify({
          action: 'search',
          query: 'test',
        }));
        messagesSent.add(1);
      }, 5000);
    });

    socket.on('message', (msg) => {
      try {
        const data = JSON.parse(msg);
        messagesReceived.add(1);

        if (data.type === 'new_mail') {
          check(data, {
            'new_mail has id': (d) => d.messageId !== undefined,
          });
        }
      } catch (e) {
        console.log(`Received: ${msg}`);
      }
    });

    socket.on('close', () => {
      console.log(`VU ${__VU}: Disconnected`);
    });

    socket.on('error', (e) => {
      console.error(`VU ${__VU} Error: ${e.error()}`);
      errorRate.add(1);
    });

    // Close after 60 seconds
    socket.setTimeout(function () {
      socket.close();
    }, 60000);
  });

  check(res, {
    'WebSocket connected': (r) => r && r.status === 101,
  });

  if (res.error) {
    errorRate.add(1);
    console.error(`Connection error: ${res.error}`);
  }

  sleep(5);
}

export function handleSummary(data) {
  return {
    'websocket-load-summary.json': JSON.stringify(data),
    stdout: `
WebSocket Load Test Summary:
============================
Connections: ${data.metrics.ws_connect_time?.count || 0}
Avg Connect Time: ${data.metrics.ws_connect_time?.avg?.toFixed(2) || 'N/A'}ms
P95 Connect Time: ${data.metrics.ws_connect_time?.['p(95)']?.toFixed(2) || 'N/A'}ms
Messages Sent: ${data.metrics.ws_messages_sent?.count || 0}
Messages Received: ${data.metrics.ws_messages_received?.count || 0}
Error Rate: ${((data.metrics.ws_error_rate?.rate || 0) * 100).toFixed(2)}%
`,
  };
}
