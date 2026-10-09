# uMailServer E2E Tests

End-to-end browser tests using Playwright.

## Test Coverage

- **Admin Panel**: Login, dashboard, domains, accounts, queue, settings
- **Account Panel**: Login, profile, password change, 2FA, forwarding
- **Webmail**: Login, compose, read, reply, forward, folders, search

## Setup

```bash
cd e2e
npm install
npm run install:browsers
```

## Running Tests

```bash
# Run all tests
npm test

# Run with UI mode
npm run test:ui

# Run specific test file
npm run test:admin
npm run test:account
npm run test:webmail

# Debug mode
npm run test:debug

# View report
npm run test:report
```

## Environment Variables

```bash
BASE_URL=http://localhost:8080 npm test
WEBSERVER_CMD="docker-compose up" npm test
```

## Test Data

Test users and domains are defined in `fixtures/users.json`.

## CI Integration

The E2E job in `.github/workflows/ci.yml` runs when a release tag (`v*`) is
pushed or when CI is started by hand (`gh workflow run ci.yml --ref <branch>`).
It does not run on every push or pull request; see
`docs/CONTRIBUTING.md`, "CI and releases".

## Writing Tests

```javascript
const { test, expect } = require('@playwright/test');

test('user can login', async ({ page }) => {
  await page.goto('/admin');
  await page.fill('input[name="email"]', 'admin@example.com');
  await page.fill('input[name="password"]', 'password');
  await page.click('button[type="submit"]');
  await expect(page).toHaveURL('/admin/dashboard');
});
```

## Screenshots

Screenshots and videos are captured on failure in `test-results/`.
