import { test, expect } from '@playwright/test';

// The tenant view (apps/web/tenant/index.html) is where a rent reminder
// lands: no framework, no build step, and it must render and stay usable
// with no backend configured — a returning tenant with a live token never
// even downloads the auth SDK. These tests run against the file as shipped,
// with no `dwellm8-api`/`gip-*` meta tags, which is exactly the "not yet
// configured" state a misconfigured deploy would produce.

test.beforeEach(async ({ page }) => {
  await page.goto('/tenant/index.html');
});

test('loads with the right title and never throws before anybody signs in', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.reload();
  await expect(page).toHaveTitle('Your rent — Dwellm8');
  expect(errors).toEqual([]);
});

test('shows the sign-in form and hides the tenancy view, loading and error states', async ({ page }) => {
  await expect(page.locator('#signin')).toBeVisible();
  await expect(page.locator('#view')).toBeHidden();
  await expect(page.locator('#loading')).toBeHidden();
  await expect(page.locator('#error')).toBeHidden();
});

test('the phone form asks for a number and nothing else before sending a code', async ({ page }) => {
  await expect(page.locator('#phone')).toBeVisible();
  await expect(page.locator('#phone')).toHaveAttribute('type', 'tel');
  await expect(page.locator('#phone')).toHaveAttribute('required', '');
  await expect(page.locator('#code-form')).toBeHidden();
  await expect(page.getByRole('button', { name: 'Send the code' })).toBeVisible();
});

test('the phone field is reachable by its label, for a tenant using a screen reader', async ({ page }) => {
  await expect(page.getByLabel('Mobile number')).toBeVisible();
});

test('accepts typed input into the phone field', async ({ page }) => {
  await page.getByLabel('Mobile number').fill('+91 98765 43210');
  await expect(page.getByLabel('Mobile number')).toHaveValue('+91 98765 43210');
});

test('submitting the phone form with no API key configured fails safely rather than crashing', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.getByLabel('Mobile number').fill('+91 98765 43210');
  await page.getByRole('button', { name: 'Send the code' }).click();

  // No gip-api-key meta tag is present on the bare file, so signing in is
  // refused by the page's own guard rather than by an unhandled exception.
  await expect(page.locator('#signin-error')).toBeVisible();
  expect(errors).toEqual([]);
});

test('renders at a phone width without introducing horizontal scroll', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 780 });
  await page.reload();
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(scrollWidth).toBeLessThanOrEqual(clientWidth + 1); // +1 for sub-pixel rounding
});

test('a tenancy fetched from the API renders what is owed', async ({ page }) => {
  // A live token in sessionStorage under dwellm8.tenant.token skips straight
  // to load(), which is the branch a signed-in return visit actually takes —
  // the token dies with the tab by design, so sessionStorage rather than
  // localStorage is the one that matters here. Mocking the API response is
  // what lets that branch run end to end without a live backend.
  await page.addInitScript(() => {
    sessionStorage.setItem('dwellm8.tenant.token', 'fake-token-for-testing');
  });
  await page.route('**/v1/resident/tenancies', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        tenancies: [{
          id: 'lease-1', property: 'Green Meadows', unit: '4B',
          owed_minor: 2500000, currency: 'INR',
        }],
      }),
    }));
  await page.goto('/tenant/index.html');

  await expect(page.locator('#loading')).toBeHidden();
  await expect(page.locator('#signin')).toBeHidden();
  await expect(page.locator('#view')).toBeVisible();
  await expect(page.locator('#who')).toHaveText('1 tenancy');
});

test('a phone number with no tenancy on it is told plainly, not shown an empty list', async ({ page }) => {
  await page.addInitScript(() => {
    sessionStorage.setItem('dwellm8.tenant.token', 'fake-token-for-testing');
  });
  await page.route('**/v1/resident/tenancies', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ tenancies: [] }) }));
  await page.goto('/tenant/index.html');

  await expect(page.locator('#error')).toBeVisible();
  await expect(page.locator('#view')).toBeHidden();
  await expect(page.locator('#retry')).toBeHidden();
});

test('an expired token sends the tenant back to sign-in rather than showing a blank error', async ({ page }) => {
  await page.addInitScript(() => {
    sessionStorage.setItem('dwellm8.tenant.token', 'stale-token');
  });
  await page.route('**/v1/resident/tenancies', (route) => route.fulfill({ status: 401 }));
  await page.goto('/tenant/index.html');

  await expect(page.locator('#signin')).toBeVisible();
  await expect(page.locator('#error')).toBeHidden();
  // Signing out clears the stale token, so a reload does not loop forever
  // retrying the same expired credential.
  expect(await page.evaluate(() => sessionStorage.getItem('dwellm8.tenant.token'))).toBeNull();
});
