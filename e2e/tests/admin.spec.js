const { test, expect } = require('@playwright/test');
const users = require('../fixtures/users.json');

test.describe('Admin Panel', () => {
  test.describe('Login/Logout', () => {
    test('admin can login with valid credentials', async ({ page }) => {
      await page.goto('/admin');

      await page.fill('input[name="email"]', users.admin.email);
      await page.fill('input[name="password"]', users.admin.password);
      await page.click('button[type="submit"]');

      await page.waitForURL('/admin/dashboard');
      await expect(page.locator('h1')).toContainText('Dashboard');
    });

    test('admin cannot login with invalid credentials', async ({ page }) => {
      await page.goto('/admin');

      await page.fill('input[name="email"]', users.admin.email);
      await page.fill('input[name="password"]', 'wrongpassword');
      await page.click('button[type="submit"]');

      await expect(page.locator('.error')).toContainText('Invalid credentials');
    });

    test('admin can logout', async ({ page, context }) => {
      // Use stored auth
      await context.addCookies([
        { name: 'auth', value: 'admin-token', domain: 'localhost', path: '/' }
      ]);

      await page.goto('/admin/dashboard');
      await page.click('text=Logout');

      await page.waitForURL('/admin');
      await expect(page.locator('h2')).toContainText('Admin Login');
    });
  });

  test.describe('Dashboard', () => {
    test.use({ storageState: 'playwright/.auth/admin.json' });

    test('dashboard shows system overview', async ({ page }) => {
      await page.goto('/admin/dashboard');

      await expect(page.locator('h1')).toContainText('Dashboard');
      await expect(page.locator('[data-testid="domain-count"]')).toBeVisible();
      await expect(page.locator('[data-testid="account-count"]')).toBeVisible();
      await expect(page.locator('[data-testid="queue-size"]')).toBeVisible();
    });

    test('navigation works', async ({ page }) => {
      await page.goto('/admin/dashboard');

      // Navigate to Domains
      await page.click('text=Domains');
      await page.waitForURL('/admin/domains');
      await expect(page.locator('h1')).toContainText('Domains');

      // Navigate to Accounts
      await page.click('text=Accounts');
      await page.waitForURL('/admin/accounts');
      await expect(page.locator('h1')).toContainText('Accounts');

      // Navigate to Queue
      await page.click('text=Queue');
      await page.waitForURL('/admin/queue');
      await expect(page.locator('h1')).toContainText('Mail Queue');
    });
  });

  test.describe('Domain Management', () => {
    test.use({ storageState: 'playwright/.auth/admin.json' });

    test('admin can create a new domain', async ({ page }) => {
      await page.goto('/admin/domains');

      // Click add domain
      await page.click('button:has-text("Add Domain")');

      // Fill form
      const domainName = `test-${Date.now()}.com`;
      await page.fill('input[name="name"]', domainName);
      await page.fill('input[name="maxAccounts"]', '10');
      await page.fill('input[name="maxAliases"]', '20');

      // Submit
      await page.click('button[type="submit"]');

      // Verify domain created
      await expect(page.locator(`text=${domainName}`)).toBeVisible();
    });

    test('admin can view domain details', async ({ page }) => {
      await page.goto('/admin/domains');

      // Click on existing domain
      await page.click(`text=${users.user.domain}`);

      await expect(page.locator('h2')).toContainText(users.user.domain);
      await expect(page.locator('[data-testid="domain-stats"]')).toBeVisible();
    });

    test('admin can delete a domain', async ({ page }) => {
      // First create a domain to delete
      await page.goto('/admin/domains');
      await page.click('button:has-text("Add Domain")');

      const domainToDelete = `delete-${Date.now()}.com`;
      await page.fill('input[name="name"]', domainToDelete);
      await page.fill('input[name="maxAccounts"]', '5');
      await page.click('button[type="submit"]');

      // Delete the domain
      await page.click(`[data-testid="delete-${domainToDelete}"]`);
      await page.click('button:has-text("Confirm")');

      // Verify deleted
      await expect(page.locator(`text=${domainToDelete}`)).not.toBeVisible();
    });

    test('cannot create duplicate domain', async ({ page }) => {
      await page.goto('/admin/domains');
      await page.click('button:has-text("Add Domain")');

      await page.fill('input[name="name"]', users.user.domain);
      await page.click('button[type="submit"]');

      await expect(page.locator('.error')).toContainText('already exists');
    });
  });

  test.describe('Account Management', () => {
    test.use({ storageState: 'playwright/.auth/admin.json' });

    test('admin can create a new account', async ({ page }) => {
      await page.goto(`/admin/domains/${users.user.domain}/accounts`);

      await page.click('button:has-text("Add Account")');

      const localPart = `user${Date.now()}`;
      await page.fill('input[name="localPart"]', localPart);
      await page.fill('input[name="password"]', 'SecurePass123!');
      await page.fill('input[name="confirmPassword"]', 'SecurePass123!');

      await page.click('button[type="submit"]');

      await expect(page.locator(`text=${localPart}@${users.user.domain}`)).toBeVisible();
    });

    test('admin can reset account password', async ({ page }) => {
      await page.goto(`/admin/domains/${users.user.domain}/accounts`);

      await page.click(`[data-testid="edit-${users.user.email}"]`);

      await page.fill('input[name="password"]', 'NewPass123!');
      await page.fill('input[name="confirmPassword"]', 'NewPass123!');

      await page.click('button[type="submit"]');

      await expect(page.locator('.success')).toContainText('updated');
    });

    test('admin can disable/enable account', async ({ page }) => {
      await page.goto(`/admin/domains/${users.user.domain}/accounts`);

      // Toggle status
      await page.click(`[data-testid="toggle-${users.user.email}"]`);

      await expect(page.locator('.success')).toContainText('updated');
    });

    test('admin can delete account', async ({ page }) => {
      // Create account to delete
      await page.goto(`/admin/domains/${users.user.domain}/accounts`);
      await page.click('button:has-text("Add Account")');

      const localPart = `delete-${Date.now()}`;
      await page.fill('input[name="localPart"]', localPart);
      await page.fill('input[name="password"]', 'TempPass123!');
      await page.fill('input[name="confirmPassword"]', 'TempPass123!');
      await page.click('button[type="submit"]');

      // Delete it
      await page.click(`[data-testid="delete-${localPart}@${users.user.domain}"]`);
      await page.click('button:has-text("Confirm")');

      await expect(page.locator(`text=${localPart}`)).not.toBeVisible();
    });
  });

  test.describe('Queue Management', () => {
    test.use({ storageState: 'playwright/.auth/admin.json' });

    test('admin can view mail queue', async ({ page }) => {
      await page.goto('/admin/queue');

      await expect(page.locator('h1')).toContainText('Mail Queue');
      await expect(page.locator('[data-testid="queue-stats"]')).toBeVisible();
    });

    test('admin can retry queued message', async ({ page }) => {
      await page.goto('/admin/queue');

      // Assume there's a message in queue
      const retryButton = page.locator('[data-testid="retry-message"]').first();
      if (await retryButton.isVisible()) {
        await retryButton.click();
        await expect(page.locator('.success')).toContainText('retry');
      }
    });

    test('admin can delete from queue', async ({ page }) => {
      await page.goto('/admin/queue');

      const deleteButton = page.locator('[data-testid="delete-message"]').first();
      if (await deleteButton.isVisible()) {
        await deleteButton.click();
        await page.click('button:has-text("Confirm")');
        await expect(page.locator('.success')).toContainText('removed');
      }
    });
  });

  test.describe('Settings', () => {
    test.use({ storageState: 'playwright/.auth/admin.json' });

    test('admin can view settings', async ({ page }) => {
      await page.goto('/admin/settings');

      await expect(page.locator('h1')).toContainText('Settings');
      await expect(page.locator('text=General')).toBeVisible();
      await expect(page.locator('text=Security')).toBeVisible();
      await expect(page.locator('text=SMTP')).toBeVisible();
    });
  });
});
