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
  await page.fill('#email', users.admin.email);
  await page.fill('#password', users.admin.password);
  await page.click('button[type="submit"]');

  // The admin SPA's basename-less router navigates to "/" after login
  // (leaving /admin/*); the auth token persists in localStorage either way.
  await page.waitForURL((u) => !u.pathname.startsWith('/admin'));

  // Save authentication state
  await page.context().storageState({ path: adminAuthFile });
});

// Setup user authentication
setup('authenticate as user', async ({ page }) => {
  // The webmail SPA's login form is served at /login (its routes live at the
  // server root; /account serves the same SPA shell without a matching route).
  await page.goto('/login');
  await page.fill('#email', users.user.email);
  await page.fill('#password', users.user.password);
  await page.click('button[type="submit"]');

  // Post-login the SPA navigates to the inbox.
  await page.waitForURL(/\/inbox/);

  // Save authentication state
  await page.context().storageState({ path: authFile });
});
