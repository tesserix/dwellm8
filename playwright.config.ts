import { defineConfig, devices } from '@playwright/test';

// Playwright drives the static pages in apps/web — the tenant view a rent
// reminder links to, and the marketing site. Neither has a build step; the
// server below just serves the files as-is, the way SETUP_LOCAL.md and
// apps/web/tenant/README.md already document running them by hand.
//
// There is no Next.js app in this repo yet (see apps/web's README pointers).
// Once one lands, it gets its own webServer entry and its own spec directory
// here rather than a rewrite of this config.

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: [['html', { open: 'never' }]],
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'on-first-retry',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile-safari', use: { ...devices['iPhone 13'] } },
  ],
  webServer: {
    command: 'npx http-server apps/web -p 4173 -c-1 --silent',
    url: 'http://127.0.0.1:4173/design/home.html',
    reuseExistingServer: !process.env.CI,
  },
});
