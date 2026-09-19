import { expect, test } from "@playwright/test"

// The full product flow from the README:
// REGISTER → LOGIN → ADD POSTGRES DATABASE → TEST CONNECTION → ADD STORAGE →
// CREATE SCHEDULE → RUN BACKUP → UPLOAD → VERIFY CHECKSUM → RESTORE →
// VERIFY RESTORE → VIEW BACKUP HISTORY
//
// The database under test must be reachable from the DBVault worker, e.g. the
// sample database started by `docker compose -f docker-compose.yml -f docker/e2e.yml up -d`.

const pg = {
  host: process.env.E2E_PG_HOST ?? "sample-postgres",
  port: process.env.E2E_PG_PORT ?? "5432",
  database: process.env.E2E_PG_DATABASE ?? "shop",
  username: process.env.E2E_PG_USER ?? "shop",
  password: process.env.E2E_PG_PASSWORD ?? "shop-password",
}

test("register, protect a database, back it up, verify and restore it", async ({ page }) => {
  const email = `e2e-${Date.now()}@example.com`
  const password = "correct-horse-battery"

  // Register
  await page.goto("/register")
  await page.getByLabel("Name").fill("E2E Tester")
  await page.getByLabel("Work email").fill(email)
  await page.locator("#password").fill(password)
  await page.getByRole("button", { name: "Create account" }).click()
  await expect(page).toHaveURL(/\/dashboard/)
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible()

  // Log out and back in
  await page.getByRole("button", { name: "Account menu" }).click()
  await page.getByRole("menuitem", { name: "Sign out" }).click()
  await expect(page).toHaveURL(/\/login/)
  await page.getByLabel("Email").fill(email)
  await page.locator("#password").fill(password)
  await page.getByRole("button", { name: "Sign in" }).click()
  await expect(page).toHaveURL(/\/dashboard/)

  // Add a PostgreSQL database and test the connection
  await page.goto("/databases/new")
  await page.getByLabel("Name", { exact: true }).fill("production")
  await page.getByLabel("Host").fill(pg.host)
  await page.getByLabel("Port").fill(pg.port)
  await page.getByLabel("Database", { exact: true }).fill(pg.database)
  await page.getByLabel("Username").fill(pg.username)
  await page.getByLabel("Password").fill(pg.password)
  await page.getByRole("combobox", { name: /SSL mode/i }).click()
  await page.getByRole("option", { name: /^disable/i }).click()
  await page.getByRole("button", { name: /Test Connection/i }).click()
  await expect(page.getByText("Connection successful")).toBeVisible()
  await expect(page.getByText(/PostgreSQL \d+/).first()).toBeVisible()
  await page.getByRole("button", { name: /Save database/i }).click()
  await expect(page).toHaveURL(/\/databases\/[0-9a-f-]{36}$/)
  const databaseUrl = page.url()

  // Add storage (built-in MinIO)
  await page.goto("/storage?new=1")
  await page.getByRole("button", { name: "Use built-in storage" }).click()
  await expect(page.getByRole("dialog")).toBeHidden()
  await expect(page.getByText("Local MinIO").first()).toBeVisible()

  // Create a schedule
  await page.goto(databaseUrl.replace(/\/databases\/(.+)$/, "/schedules?new=1&database=$1"))
  const scheduleDialog = page.getByRole("dialog")
  await expect(scheduleDialog.getByText("Create schedule").first()).toBeVisible()
  await scheduleDialog.getByRole("button", { name: "Create schedule" }).click()
  await expect(scheduleDialog).toBeHidden()
  await expect(page.getByRole("cell", { name: /production/ }).first()).toBeVisible()

  // Run a backup and follow it live
  await page.goto(databaseUrl)
  await page.getByRole("button", { name: /Run backup/i }).first().click()
  await expect(page).toHaveURL(/\/backups\/[0-9a-f-]{36}$/)
  await expect(page.getByText("Checksum verified").first()).toBeVisible({ timeout: 120_000 })
  await expect(page.getByText("Backup completed").first()).toBeVisible()
  const backupUrl = page.url()

  // Verify the backup (checksum + restore test in the sandbox)
  await page.getByRole("button", { name: /Verify backup/i }).click()
  const verification = page.locator("[data-slot=card]").filter({ hasText: "Recovery test duration" })
  await expect(verification).toBeVisible({ timeout: 180_000 })
  for (const check of ["Backup integrity", "Restore test", "Database verification"]) {
    await expect(verification.getByText(check, { exact: true })).toBeVisible()
  }
  // Every check must pass; "unavailable" would mean the sandbox isn't configured.
  await expect(verification.getByText("PASS", { exact: true })).toHaveCount(3)

  // Restore into a new database
  await page.goto(backupUrl.replace(/\/backups\/(.+)$/, "/restore?backup=$1"))
  const restoredName = `shop_e2e_${Date.now()}`
  await page.getByText("Restore into a new database").click()
  await page.getByLabel("New database name").fill(restoredName)
  await page.getByRole("button", { name: "Review and restore" }).click()
  await page.getByRole("alertdialog").getByRole("button", { name: "Start restore" }).click()
  // The restore detail panel follows the job through to completion.
  await expect(page.getByText(/Verified \d+ of \d+ tables present/).first()).toBeVisible({ timeout: 180_000 })
  await expect(page.getByText("Completed").first()).toBeVisible()

  // Backup history shows the completed backup
  await page.goto("/backups")
  await expect(page.getByRole("cell", { name: "production" }).first()).toBeVisible()
  await expect(page.getByText("Completed").first()).toBeVisible()
})
