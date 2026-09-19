import { defineConfig, devices } from "@playwright/test"

// End-to-end tests run against a running DBVault stack (docker compose up).
//   E2E_BASE_URL          default http://localhost:3000
//   E2E_PG_HOST/PORT/...  a PostgreSQL database the stack's worker can reach
export default defineConfig({
  testDir: "./e2e",
  timeout: 5 * 60_000,
  expect: { timeout: 30_000 },
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
})
