#!/usr/bin/env node
/**
 * globalTeardown.js - E2E test cleanup
 *
 * This script cleans up after E2E tests by killing the server started in globalSetup.
 */

if (global.__umailserverProc) {
  console.log('Stopping server...');
  global.__umailserverProc.kill();
  delete global.__umailserverProc;
}

console.log('=== E2E Teardown Complete ===');
