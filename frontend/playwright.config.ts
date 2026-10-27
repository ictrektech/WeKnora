import { defineConfig } from '@playwright/test'

const externalBaseURL = process.env.PLAYWRIGHT_BASE_URL
const baseURL = externalBaseURL || 'http://127.0.0.1:5174'

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  workers: 2,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  // Initial route assertions also wait for Vite's lazy component compilation.
  expect: { timeout: 15_000 },
  reporter: 'line',
  use: {
    baseURL,
    trace: 'retain-on-failure',
  },
  webServer: externalBaseURL ? undefined : {
    command: 'npm run dev -- --host 127.0.0.1 --port 5174 --strictPort',
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
  },
})
