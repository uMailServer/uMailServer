const { test, expect } = require('@playwright/test');
const users = require('../fixtures/users.json');

test.describe('Webmail', () => {
  test.describe('Login/Logout', () => {
    // The webmail SPA's routes are served at the server root (/login, /inbox);
    // /webmail/* is not a routed path. The login inputs carry ids, not names,
    // and login.tsx renders the literal error "Invalid email or password".
    test('user can login to webmail', async ({ page }) => {
      await page.goto('/login');

      await page.fill('#email', users.user.email);
      await page.fill('#password', users.user.password);
      await page.click('button[type="submit"]');

      await page.waitForURL(/\/inbox/);
    });

    test('webmail shows error on invalid login', async ({ page }) => {
      await page.goto('/login');

      await page.fill('#email', users.user.email);
      await page.fill('#password', 'wrongpassword');
      await page.click('button[type="submit"]');

      await expect(page.getByText('Invalid email or password')).toBeVisible();
    });
  });

  // The flows below were authored before the suite ever ran and target markup
  // the real inbox UI does not render: inbox.tsx has no data-testid attributes
  // and no <h1> headings, and the compose/read flows hit a mock store with no
  // API wiring. They are kept as documentation of the intended coverage and
  // need testids added to the UI (or the specs rewritten against the real
  // components) before they can run.
  test.describe('Inbox', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test.fixme('inbox loads and shows email list', async ({ page }) => {
      await page.goto('/inbox');

      await expect(page.locator('h1')).toContainText('Inbox');
      await expect(page.locator('[data-testid="email-list"]')).toBeVisible();
    });

    test.fixme('user can refresh inbox', async ({ page }) => {
      await page.goto('/inbox');

      await page.click('[data-testid="refresh-btn"]');

      await expect(page.locator('[data-testid="loading"]')).not.toBeVisible();
      await expect(page.locator('[data-testid="email-list"]')).toBeVisible();
    });

    test.fixme('user can navigate between pages', async ({ page }) => {
      await page.goto('/inbox');

      // Click next page if available
      const nextBtn = page.locator('[data-testid="next-page"]');
      if (await nextBtn.isVisible() && await nextBtn.isEnabled()) {
        await nextBtn.click();
        await expect(page.locator('[data-testid="email-list"]')).toBeVisible();
      }
    });

    test.fixme('user can search emails', async ({ page }) => {
      await page.goto('/inbox');

      await page.fill('[data-testid="search-input"]', 'test');
      await page.press('[data-testid="search-input"]', 'Enter');

      await expect(page.locator('[data-testid="search-results"]')).toBeVisible();
    });
  });

  test.describe('Compose Email', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test.fixme('user can compose and send email', async ({ page }) => {
      await page.goto('/inbox');

      // Open compose
      await page.click('button:has-text("Compose")');

      // Fill form
      await page.fill('input[name="to"]', 'recipient@example.com');
      await page.fill('input[name="subject"]', 'Test Subject');
      await page.fill('[data-testid="email-body"]', 'This is a test email body.');

      // Send
      await page.click('button:has-text("Send")');

      await expect(page.locator('.success')).toContainText('sent');
    });

    test.fixme('user can save draft', async ({ page }) => {
      await page.goto('/inbox');

      await page.click('button:has-text("Compose")');

      await page.fill('input[name="to"]', 'draft@example.com');
      await page.fill('input[name="subject"]', 'Draft Subject');
      await page.fill('[data-testid="email-body"]', 'Draft body.');

      await page.click('button:has-text("Save Draft")');

      await expect(page.locator('.success')).toContainText('saved');
    });

    test.fixme('compose validates required fields', async ({ page }) => {
      await page.goto('/inbox');

      await page.click('button:has-text("Compose")');

      // Try to send without filling
      await page.click('button:has-text("Send")');

      await expect(page.locator('.error')).toContainText('required');
    });

    test.fixme('user can add attachments', async ({ page }) => {
      await page.goto('/inbox');

      await page.click('button:has-text("Compose")');

      // Upload file
      const fileInput = page.locator('input[type="file"]');
      await fileInput.setInputFiles({
        name: 'test.txt',
        mimeType: 'text/plain',
        buffer: Buffer.from('test content')
      });

      await expect(page.locator('text=test.txt')).toBeVisible();
    });
  });

  test.describe('Read Email', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test.fixme('user can open and read email', async ({ page }) => {
      await page.goto('/inbox');

      // Click first email
      const firstEmail = page.locator('[data-testid="email-item"]').first();
      if (await firstEmail.isVisible()) {
        await firstEmail.click();

        await expect(page.locator('[data-testid="email-detail"]')).toBeVisible();
        await expect(page.locator('[data-testid="email-subject"]')).toBeVisible();
        await expect(page.locator('[data-testid="email-body"]')).toBeVisible();
      }
    });

    test.fixme('user can reply to email', async ({ page }) => {
      await page.goto('/inbox');

      const firstEmail = page.locator('[data-testid="email-item"]').first();
      if (await firstEmail.isVisible()) {
        await firstEmail.click();

        await page.click('button:has-text("Reply")');

        await page.fill('[data-testid="reply-body"]', 'This is my reply.');
        await page.click('button:has-text("Send")');

        await expect(page.locator('.success')).toContainText('sent');
      }
    });

    test.fixme('user can forward email', async ({ page }) => {
      await page.goto('/inbox');

      const firstEmail = page.locator('[data-testid="email-item"]').first();
      if (await firstEmail.isVisible()) {
        await firstEmail.click();

        await page.click('button:has-text("Forward")');

        await page.fill('input[name="to"]', 'forward@example.com');
        await page.click('button:has-text("Send")');

        await expect(page.locator('.success')).toContainText('sent');
      }
    });

    test.fixme('user can delete email', async ({ page }) => {
      await page.goto('/inbox');

      const firstEmail = page.locator('[data-testid="email-item"]').first();
      if (await firstEmail.isVisible()) {
        // Check the checkbox
        await firstEmail.locator('input[type="checkbox"]').check();

        // Click delete
        await page.click('button:has-text("Delete")');

        await expect(page.locator('.success')).toContainText('deleted');
      }
    });
  });

  test.describe('Folders', () => {
    test.use({ storageState: 'playwright/.auth/user.json' });

    test.fixme('user can view different folders', async ({ page }) => {
      await page.goto('/inbox');

      // Navigate to Sent
      await page.click('text=Sent');
      await expect(page.locator('h1')).toContainText('Sent');

      // Navigate to Drafts
      await page.click('text=Drafts');
      await expect(page.locator('h1')).toContainText('Drafts');

      // Navigate to Trash
      await page.click('text=Trash');
      await expect(page.locator('h1')).toContainText('Trash');
    });

    test.fixme('user can create new folder', async ({ page }) => {
      await page.goto('/inbox');

      await page.click('button:has-text("New Folder")');

      const folderName = `TestFolder-${Date.now()}`;
      await page.fill('input[name="folderName"]', folderName);
      await page.click('button:has-text("Create")');

      await expect(page.locator(`text=${folderName}`)).toBeVisible();
    });
  });

  test.describe('Mobile Viewport', () => {
    test.use({
      storageState: 'playwright/.auth/user.json',
      viewport: { width: 375, height: 667 }
    });

    test.fixme('webmail is usable on mobile', async ({ page }) => {
      await page.goto('/inbox');

      await expect(page.locator('h1')).toContainText('Inbox');
      await expect(page.locator('[data-testid="email-list"]')).toBeVisible();

      // Mobile menu should be accessible
      await page.click('[data-testid="mobile-menu-btn"]');
      await expect(page.locator('text=Compose')).toBeVisible();
    });
  });
});
