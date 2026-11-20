import { defineConfig, devices } from "@playwright/test";

// e2e runs the real gateway with mock providers only (no API keys, no quota)
// and a production build of the dashboard pointed at it.
import { E2E_KEY as KEY, GATEWAY_PORT, GATEWAY_URL, WEB_PORT } from "./e2e/env";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false, // tests share one gateway and its in-memory metrics
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0, // a retry would see the first run's in-memory metrics
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: `http://127.0.0.1:${WEB_PORT}`,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: `go run ./cmd/doorkey -config web/e2e/doorkey.e2e.json -addr 127.0.0.1:${GATEWAY_PORT}`,
      cwd: "..",
      url: `${GATEWAY_URL}/healthz`,
      env: { DOORKEY_API_KEY: KEY, DOORKEY_LOG_LEVEL: "warn" },
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: `npm run build && npx next start -H 127.0.0.1 -p ${WEB_PORT}`,
      url: `http://127.0.0.1:${WEB_PORT}`,
      env: { DOORKEY_URL: GATEWAY_URL, DOORKEY_API_KEY: KEY },
      reuseExistingServer: false,
      timeout: 180_000,
    },
  ],
});
