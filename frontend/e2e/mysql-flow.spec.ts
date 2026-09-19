import { expect, test } from "@playwright/test"

// The backup flow for MySQL and MariaDB: ADD DATABASE → TEST CONNECTION →
// RUN BACKUP → VERIFY (sandbox restore) → RESTORE INTO A NEW DATABASE.
//
// Uses the sample databases from `docker compose -f docker-compose.yml -f docker/e2e.yml up -d`.

const engines = [
  { label: "MySQL", host: process.env.E2E_MYSQL_HOST ?? "sample-mysql", port: process.env.E2E_MYSQL_PORT ?? "3306" },
  { label: "MariaDB", host: process.env.E2E_MARIADB_HOST ?? "sample-mariadb", port: process.env.E2E_MARIADB_PORT ?? "3306" },
]

for (const engine of engines) {
  test(`back up, verify and restore a ${engine.label} database`, async ({ page }) => {
    // Register a fresh account and add the built-in storage.
    await page.goto("/register")
    await page.getByLabel("Name").fill("E2E Tester")
    await page.getByLabel("Work email").fill(`e2e-${engine.label.toLowerCase()}-${Date.now()}@example.com`)
    await page.getByLabel("Password").fill("correct-horse-battery")
    await page.getByRole("button", { name: "Create account" }).click()
    await expect(page).toHaveURL(/\/dashboard/)
    await page.goto("/storage?new=1")
    await page.getByRole("button", { name: "Use built-in storage" }).click()
    await expect(page.getByRole("dialog")).toBeHidden()

    // Add the database
    await page.goto("/databases/new")
    await page.getByRole("radio", { name: new RegExp(engine.label) }).click()
    await expect(page.getByLabel("Port")).toHaveValue("3306")
    await expect(page.getByText("GRANT SELECT, SHOW VIEW")).toBeVisible()
    await page.getByLabel("Name", { exact: true }).fill("shop")
    await page.getByLabel("Host").fill(engine.host)
    await page.getByLabel("Port").fill(engine.port)
    await page.getByLabel("Database", { exact: true }).fill("shop")
    await page.getByLabel("Username").fill("shop")
    await page.getByLabel("Password").fill("shop-password")
    await page.getByRole("button", { name: /Test Connection/i }).click()
    await expect(page.getByText("Connection successful")).toBeVisible()
    await expect(page.getByText(new RegExp(`${engine.label} \\d+`)).first()).toBeVisible()
    await page.getByRole("button", { name: /Save database/i }).click()
    await expect(page).toHaveURL(/\/databases\/[0-9a-f-]{36}$/)

    // Back up
    await page.getByRole("button", { name: /Run backup/i }).first().click()
    await expect(page).toHaveURL(/\/backups\/[0-9a-f-]{36}$/)
    await expect(page.getByText("Checksum verified").first()).toBeVisible({ timeout: 120_000 })
    await expect(page.getByText("Backup completed").first()).toBeVisible()
    await expect(page.getByText("mariadb-dump").first()).toBeVisible()
    const backupUrl = page.url()

    // Verify in a sandbox of the same engine
    await page.getByRole("button", { name: /Verify backup/i }).click()
    const verification = page.locator("[data-slot=card]").filter({ hasText: "Recovery test duration" })
    await expect(verification).toBeVisible({ timeout: 180_000 })
    await expect(verification.getByText("PASS", { exact: true })).toHaveCount(3)

    // Restore into a new database; the wizard warns that MySQL restores aren't transactional.
    await page.goto(backupUrl.replace(/\/backups\/(.+)$/, "/restore?backup=$1"))
    await page.getByText("Restore into a new database").click()
    await page.getByLabel("New database name").fill(`shop_e2e_${Date.now()}`)
    await page.getByRole("button", { name: "Review and restore" }).click()
    const confirm = page.getByRole("alertdialog")
    await expect(confirm.getByText(/Not transactional/)).toBeVisible()
    await confirm.getByRole("button", { name: "Start restore" }).click()
    await expect(page.getByText(/Verified \d+ of \d+ tables present/).first()).toBeVisible({ timeout: 180_000 })
    await expect(page.getByText("Completed").first()).toBeVisible()
  })
}
