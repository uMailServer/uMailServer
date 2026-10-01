#!/usr/bin/env node
/**
 * globalSetup.js - E2E test initialization
 *
 * This script initializes the E2E test environment by:
 * 1. Removing any existing test data
 * 2. Running the uMailServer quickstart command to create initial domain/users
 * 3. Creating a test config that uses the quickstart data directory
 * 4. Starting the server and waiting for it to be ready
 */

const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const E2E_DIR = __dirname;
const DATA_DIR = path.join(E2E_DIR, 'data');
const CONFIG_FILE = path.join(E2E_DIR, '..', 'config', 'test.yaml');

async function main() {
  console.log('=== E2E Test Setup ===');

  // Clean up existing test data
  console.log('Cleaning up existing test data...');
  try {
    fs.rmSync(DATA_DIR, { recursive: true, force: true });
  } catch (e) {
    // Ignore
  }
  try {
    fs.rmSync(path.join(E2E_DIR, 'umailserver.yaml'), { force: true });
  } catch (e) {
    // Ignore
  }

  const configDir = path.dirname(CONFIG_FILE);
  if (!fs.existsSync(configDir)) {
    fs.mkdirSync(configDir, { recursive: true });
  }

  // Find server binary
  const possibleBinPaths = [
    path.join(E2E_DIR, 'umailserver.exe'),
    path.join(E2E_DIR, 'umailserver'),
    path.join(E2E_DIR, '..', 'umailserver.exe'),
    path.join(E2E_DIR, '..', 'umailserver'),
  ];

  let serverBin = possibleBinPaths.find(p => fs.existsSync(p));
  if (!serverBin) {
    console.log('Server binary not found');
    process.exit(1);
  }

  console.log('Running quickstart...');
  console.log('Binary:', serverBin);

  // Run quickstart with retry
  await runQuickstartWithRetry(serverBin);

  // Provision the fixture users the specs log in as
  await provisionFixtureUsers(serverBin);

  // Create test config
  const testConfig = `server:
  hostname: localhost
  data_dir: ./data
database:
  path: ./data/umailserver.db
http:
  enabled: true
  port: 8080
  bind: "0.0.0.0"
metrics:
  enabled: false
  port: 0
smtp:
  inbound:
    enabled: false
  submission:
    enabled: false
  submission_tls:
    enabled: false
imap:
  enabled: false
pop3:
  enabled: false
tls:
  cert_file: ""
  key_file: ""
  min_version: "1.2"
  acme:
    enabled: false
`;
  fs.writeFileSync(CONFIG_FILE, testConfig);
  console.log('Test config written to:', CONFIG_FILE);

  // Start server and wait for ready
  console.log('Starting server...');
  const serverProc = spawn(serverBin, ['serve', '--config', CONFIG_FILE], {
    cwd: E2E_DIR,
    stdio: 'pipe',
  });

  // Store for cleanup
  global.__umailserverProc = serverProc;

  serverProc.stdout.on('data', (data) => process.stdout.write(data));
  serverProc.stderr.on('data', (data) => process.stderr.write(data));

  // Wait for server to be ready
  await waitForServer(120000);

  console.log('=== E2E Setup Complete ===');

  // Playwright runs this script as its webServer command, so the process
  // must stay alive while the tests run — Playwright waits for BASE_URL,
  // then terminates us when done. Expose the same behaviour if the server
  // dies on its own, so a broken boot surfaces as "exited early".
  await new Promise((resolve) => {
    const shutdown = () => {
      console.log('Shutting down server...');
      try {
        serverProc.kill('SIGTERM');
      } catch (e) {
        // Already exited
      }
      resolve();
    };
    process.once('SIGINT', shutdown);
    process.once('SIGTERM', shutdown);
    serverProc.once('exit', (code) => {
      console.log('Server process exited early (code ' + code + '); stopping webServer command.');
      resolve();
    });
  });
}

const users = require('./fixtures/users.json');

// The E2E specs log in as the fixture accounts; quickstart only creates the
// admin, so provision the remaining users here (single password prompt).
async function provisionFixtureUsers(serverBin) {
  const isWindows = process.platform === 'win32';
  const u = users.user;
  console.log('Provisioning ' + u.email + '...');
  await new Promise((resolve, reject) => {
    const proc = spawn(serverBin, ['account', 'add', u.email, '-data-dir', './data'], {
      cwd: E2E_DIR,
      stdio: ['pipe', 'inherit', 'inherit'],
      shell: isWindows ? 'cmd' : false
    });
    proc.on('close', (code) => (code === 0 ? resolve() : reject(new Error('account add ' + u.email + ' exited ' + code))));
    proc.on('error', reject);
    proc.stdin.write(u.password + '\n');
  });
}

async function runQuickstartWithRetry(serverBin) {
  const isWindows = process.platform === 'win32';

  for (let attempt = 0; attempt < 2; attempt++) {
    if (attempt > 0) {
      console.log('Quickstart failed, retrying...');
      // Clean up before retry
      try {
        fs.rmSync(path.join(E2E_DIR, 'umailserver.yaml'), { force: true });
      } catch (e) { /* ignore */ }
    }

    const result = await new Promise((resolve) => {
      // Point quickstart at the same writable data dir the test config uses,
      // so DKIM keys and state land in e2e/data instead of /var/lib/umailserver
      // (hardcoded default; nonexistent and unwritable on CI runners), and at
      // a writable -config path (the /etc default doesn't exist either).
      const proc = spawn(serverBin, ['quickstart', '-config', './umailserver.yaml', '-data-dir', './data', 'admin@example.com'], {
        cwd: E2E_DIR,
        stdio: 'pipe',
        shell: isWindows ? 'cmd' : false
      });

      let passwordSent = false;

      proc.stdout.on('data', (data) => {
        process.stdout.write(data);
        const output = data.toString();
        if (!passwordSent) {
          if (output.includes('Overwrite')) {
            proc.stdin.write('y\n');
          } else if (output.includes('Enter admin password')) {
            proc.stdin.write(users.admin.password + '\n' + users.admin.password + '\n');
            passwordSent = true;
          }
        }
      });

      proc.stderr.on('data', (data) => process.stderr.write(data));

      proc.on('close', (code) => resolve({ code, proc }));
      proc.on('error', (err) => resolve({ code: -1, error: err }));
    });

    if (result.code === 0) {
      console.log('Quickstart succeeded');
      return;
    }

    console.log('Quickstart exit code:', result.code);
  }

  throw new Error('Quickstart failed after retries');
}

async function waitForServer(timeoutMs) {
  const startTime = Date.now();
  const baseURL = process.env.BASE_URL || 'http://localhost:8080';
  const healthURL = baseURL + '/health';

  console.log('Waiting for server to be ready...');
  console.log('  Health endpoint:', healthURL);

  while (Date.now() - startTime < timeoutMs) {
    try {
      const response = await fetch(healthURL);
      if (response.ok) {
        const data = await response.json();
        if (data.status === 'healthy' || data.status === 'ready') {
          console.log('  Server is ready!');
          return;
        }
      }
    } catch (e) {
      // Server not ready yet
    }
    await new Promise(r => setTimeout(r, 1000));
  }

  throw new Error(`Server did not become ready within ${timeoutMs}ms`);
}

main().catch(e => {
  console.error('Setup failed:', e);
  if (global.__umailserverProc) {
    global.__umailserverProc.kill();
  }
  process.exit(1);
});
