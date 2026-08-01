import { test, expect } from '@playwright/test';

// The Ops app's static web export (apps/ops, exported to apps/ops/dist by
// build:web), run against the ops-web project's own baseURL (127.0.0.1:4174,
// see playwright.config.ts). EXPO_PUBLIC_API_URL is not set at export time,
// so this exercises demo mode — the figures §9.6 ships and every install
// renders with no network — which is exactly what a smoke test wants: no
// live API dependency, deterministic content.

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('the Today screen loads with no console errors', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.reload();
  await expect(page.getByText('Good morning, Ritika')).toBeVisible();
  expect(errors).toEqual([]);
});

test('greets the signed-in manager and shows the demonstration portfolio', async ({ page }) => {
  await expect(page.getByText('Good morning, Ritika')).toBeVisible();
  // The rent-roll card: demo mode has no live roster, so the headline card
  // still renders the §9.6 figures rather than a blank or a loading spinner.
  await expect(page.getByText('Collected in July')).toBeVisible();
});

test('the footnote says plainly that this is demonstration data', async ({ page }) => {
  await expect(page.getByText(/Demonstration data\./)).toBeVisible();
});

test('the worklist and waiting-on-someone-else sections render their demo rows', async ({ page }) => {
  await expect(page.getByText('Arrears call — 12 days late')).toBeVisible();
});
