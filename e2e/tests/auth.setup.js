const { test: setup, expect } = require('@playwright/test');
const fs = require('fs');
const path = require('path');
const users = require('../fixtures/users.json');

const authFile = path.join(__dirname, '../playwright/.auth/user.json');
const adminAuthFile = path.join(__dirname, '../playwright/.auth/admin.json');

// Setup admin authentication
setup('authenticate as admin', async ({ page }) => {
  await page.goto('/admin');

  // Login
  await page.fill('input[name="email"]', users.admin.email);
  await page.fill('input[name="password"]', users.admin.password);
  await page.click('button[type="submit"]');

  // Wait for navigation to dashboard
  await page.waitForURL('/admin/dashboard');
  await expect(page.locator('h1')).toContainText('Dashboard');

  // Save authentication state
  await page.context().storageState({ path: adminAuthFile });
});

// Setup user authentication
setup('authenticate as user', async ({ page }) => {
  await page.goto('/account');

  // Login
  await page.fill('input[name="email"]', users.user.email);
  await page.fill('input[name="password"]', users.user.password);
  await page.click('button[type="submit"]');

  // Wait for navigation to profile
  await page.waitForURL('/account/profile');
  await expect(page.locator('h1')).toContainText('Profile');

  // Save authentication state
  await page.context().storageState({ path: authFile });
});
