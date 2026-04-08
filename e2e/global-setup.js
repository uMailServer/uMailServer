#!/usr/bin/env node
/**
 * globalSetup.js - E2E test initialization
 *
 * This script initializes the E2E test environment by:
 * 1. Removing any existing test data
 * 2. Running the uMailServer quickstart command to create initial domain/users
 * 3. Creating a test config that uses the quickstart data directory
 */

const { spawn, execFile } = require('child_process');
const fs = require('fs');
const path = require('path');

const E2E_DIR = __dirname;
const DATA_DIR = path.join(E2E_DIR, 'data');
// Config path must match CI workflow's WEBSERVER_CMD: "../umailserver serve --config ../config/test.yaml"
// So from e2e/, we need ../config/test.yaml which is at the repo root
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
  // Also remove the quickstart config if it exists
  try {
    fs.rmSync(path.join(E2E_DIR, 'umailserver.yaml'), { force: true });
  } catch (e) {
    // Ignore
  }

  // Ensure config directory exists
  const configDir = path.dirname(CONFIG_FILE);
  if (!fs.existsSync(configDir)) {
    fs.mkdirSync(configDir, { recursive: true });
  }

  // Find the server binary - check for pre-built one in parent directory
  // On Windows, Go builds to .exe but the output name might not include .exe
  // Check multiple possible names and locations
  const possibleBinPaths = [
    path.join(E2E_DIR, 'umailserver.exe'),
    path.join(E2E_DIR, 'umailserver'),
    path.join(E2E_DIR, '..', 'umailserver.exe'),
    path.join(E2E_DIR, '..', 'umailserver'),
  ];

  let serverBin = possibleBinPaths.find(p => fs.existsSync(p));
  if (!serverBin) {
    console.log('Server binary not found, building...');
    // We need to build - but that requires Go which may not be available
    // In CI, the binary is downloaded from artifacts
    console.log('Please ensure the server binary is built before running tests');
    process.exit(1);
  }

  // Run quickstart non-interactively
  // The quickstart command:
  // 1. Asks "Overwrite? (y/N): " if config exists
  // 2. Asks "Enter admin password: "
  // 3. Asks "Confirm password: "
  console.log('Running quickstart...');
  console.log('Binary:', serverBin);

  return new Promise((resolve, reject) => {
    // Use spawn with shell:true and pass input via stdin
    const isWindows = process.platform === 'win32';
    const proc = spawn(serverBin, ['quickstart', 'admin@example.com'], {
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
          proc.stdin.write('Admin123!\nAdmin123!\n');
          passwordSent = true;
        }
      }
    });

    proc.stderr.on('data', (data) => {
      process.stderr.write(data);
    });

    proc.on('close', (code) => {
      if (code === 0) {
        console.log('Quickstart completed successfully');

        // Create test config that uses the quickstart data
        // data_dir is relative to where the server runs (e2e/ directory)
        const testConfig = `server:
  hostname: localhost
  data_dir: ./data
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
        console.log('=== E2E Setup Complete ===');
        resolve();
      } else {
        reject(new Error(`quickstart exited with code ${code}`));
      }
    });

    proc.on('error', reject);

    // Timeout after 60 seconds
    setTimeout(() => {
      proc.kill();
      reject(new Error('quickstart timed out'));
    }, 60000);
  });
}

main().catch(e => {
  console.error('Setup failed:', e);
  process.exit(1);
});
