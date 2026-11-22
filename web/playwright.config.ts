import { defineConfig, devices } from '@playwright/test'

// The tests run against the real Go server (not a mock): e2e/start-server.sh
// seeds a fresh demo DB and serves web/dist on :7171.
// Set LOUPE_REUSE=1 to reuse a server you already started on :7171.
const PORT = 7171

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [['list']],
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    viewport: { width: 1440, height: 900 },
    colorScheme: 'light',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'e2e',
      testIgnore: /screenshots\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } },
    },
    {
      name: 'screenshots',
      testMatch: /screenshots\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } },
    },
  ],
  webServer: {
    command: 'bash e2e/start-server.sh',
    url: `http://127.0.0.1:${PORT}/healthz`,
    reuseExistingServer: process.env.LOUPE_REUSE === '1',
    timeout: 120_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
