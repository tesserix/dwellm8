import { defineConfig, devices } from '@playwright/test';

// Playwright drives the static pages in apps/web — the tenant view a rent
// reminder links to, and the marketing site — plus the Ops app's static web
// export. Neither apps/web nor the export has a live build step at request
// time; both servers below serve files already produced, the way
// SETUP_LOCAL.md and apps/web/tenant/README.md document apps/web and
// apps/ops/package.json's build:web produces the Ops export.
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
    { name: 'chromium', testIgnore: /ops-.*\.spec\.ts/, use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile-safari', testIgnore: /ops-.*\.spec\.ts/, use: { ...devices['iPhone 13'] } },
    {
      name: 'ops-web',
      testMatch: /ops-.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:4174' },
    },
  ],
  webServer: [
    {
      command: 'npx http-server apps/web -p 4173 -c-1 --silent',
      url: 'http://127.0.0.1:4173/design/home.html',
      reuseExistingServer: !process.env.CI,
    },
    {
      // The Expo static export, not `expo start`: a bundled dev server has
      // no fixed "ready" response to poll and is slow enough in CI to make
      // the suite flaky. A build-then-serve step is deterministic instead.
      command: 'npm --prefix apps/ops run build:web && npx http-server apps/ops/dist -p 4174 -c-1 --silent',
      url: 'http://127.0.0.1:4174',
      timeout: 120_000,
      reuseExistingServer: !process.env.CI,
    },
  ],
});
