const { test, expect } = require('@playwright/test');
const users = require('../fixtures/users.json');

test.describe('Account Panel', () => {
  test.describe('Login/Logout', () => {
    test('user can login with valid credentials', async ({ page }) => {
      await page.goto('/account');

      await page.fill('input[name="email"]', users.user.email);
      await page.fill('input[name="password"]', users.user.password);
      await page.click('button[type="submit"]');

      await page.waitForURL('/account/profile');
      await expect(page.locator('h1')).toContainText('Profile');
    });

    test('user cannot login with invalid credentials', async ({ page }) => {
      await page.goto('/account');

      await page.fill('input[name="email"]', users.user.email);
      await page.fill('input[name="password"]', 'wrongpassword');
      await page.click('button[type="submit"]');

      await expect(page.locator('.error')).toContainText('Invalid credentials');
    });

    test('user can logout', async ({ page, context }) => {
      await context.addCookies([
        { name: 'auth', value: 'user-token', domain: 'localhost', path: '/' }
      ]);

      await page.goto('/account/profile');
      await page.click('text=Logout');

      await page.waitForURL('/account');
      await expect(page.locator('h2')).toContainText('Login');
    });
  });

  test.describe('Profile Management', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test('user can view profile', async ({ page }) => {
      await page.goto('/account/profile');

      await expect(page.locator('h1')).toContainText('Profile');
      await expect(page.locator(`text=${users.user.email}`)).toBeVisible();
      await expect(page.locator('[data-testid="quota-usage"]')).toBeVisible();
    });

    test('user can change display name', async ({ page }) => {
      await page.goto('/account/profile');

      await page.fill('input[name="displayName"]', 'Test User Updated');
      await page.click('button[type="submit"]');

      await expect(page.locator('.success')).toContainText('updated');
    });

    test('user can change password', async ({ page }) => {
      await page.goto('/account/profile');

      await page.fill('input[name="currentPassword"]', users.user.password);
      await page.fill('input[name="newPassword"]', 'NewSecurePass123!');
      await page.fill('input[name="confirmPassword"]', 'NewSecurePass123!');
      await page.click('button[type="submit"]');

      await expect(page.locator('.success')).toContainText('password');
    });

    test('user cannot change password with wrong current password', async ({ page }) => {
      await page.goto('/account/profile');

      await page.fill('input[name="currentPassword"]', 'wrongpassword');
      await page.fill('input[name="newPassword"]', 'NewSecurePass123!');
      await page.fill('input[name="confirmPassword"]', 'NewSecurePass123!');
      await page.click('button[type="submit"]');

      await expect(page.locator('.error')).toContainText('current password');
    });
  });

  test.describe('Forwarding', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test('user can set forwarding address', async ({ page }) => {
      await page.goto('/account/forwarding');

      await page.fill('input[name="forwardTo"]', 'forward@example.com');
      await page.click('button[type="submit"]');

      await expect(page.locator('.success')).toContainText('updated');
    });

    test('user can disable forwarding', async ({ page }) => {
      await page.goto('/account/forwarding');

      await page.click('input[name="enabled"]');
      await page.click('button[type="submit"]');

      await expect(page.locator('.success')).toContainText('updated');
    });
  });

  test.describe('2FA/TOTP', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test('user can setup 2FA', async ({ page }) => {
      await page.goto('/account/2fa');

      // Start setup
      await page.click('button:has-text("Enable 2FA")');

      // Should show QR code
      await expect(page.locator('[data-testid="totp-qr"]')).toBeVisible();
      await expect(page.locator('[data-testid="totp-secret"]')).toBeVisible();

      // Enter TOTP code to verify
      await page.fill('input[name="totpCode"]', '123456');
      await page.click('button:has-text("Verify")');

      // Should show backup codes
      await expect(page.locator('[data-testid="backup-codes"]')).toBeVisible();
    });

    test('user can disable 2FA', async ({ page }) => {
      await page.goto('/account/2fa');

      await page.click('button:has-text("Disable 2FA")');

      // Confirm
      await page.click('button:has-text("Confirm")');

      await expect(page.locator('.success')).toContainText('disabled');
    });
  });

  test.describe('Navigation', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test('sidebar navigation works', async ({ page }) => {
      await page.goto('/account/profile');

      // Navigate to Forwarding
      await page.click('text=Forwarding');
      await page.waitForURL('/account/forwarding');
      await expect(page.locator('h1')).toContainText('Forwarding');

      // Navigate to 2FA
      await page.click('text=Two-Factor');
      await page.waitForURL('/account/2fa');
      await expect(page.locator('h1')).toContainText('Two-Factor');

      // Navigate back to Profile
      await page.click('text=Profile');
      await page.waitForURL('/account/profile');
      await expect(page.locator('h1')).toContainText('Profile');
    });
  });
});
