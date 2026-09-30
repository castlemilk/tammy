import {
  normalizeAccessibilitySnapshot,
  validateScreenshotFixture,
} from "../../../../scripts/check-app-store-screenshots.mjs";
import screenshotFixture from "../../release/macos/screenshots/fixture.json" with { type: "json" };
import { expect, test } from "./fixtures";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";
import { callPairedApi, runPairedMcpSmoke } from "./support/local-api";

test("runs the current accounting workflows through the packaged app", async ({
  electronHarness,
}) => {
  await setupAndRunCurrentAccountingWorkflow(electronHarness.page, electronHarness, {
    fixedRendererClock: true,
  });
  const fixture = validateScreenshotFixture(screenshotFixture);
  const page = electronHarness.currentPage();
  const targets = [
    { heading: "Overview", link: "Overview", ready: "Nothing needs review" },
    {
      heading: "Documents",
      link: "Documents",
      ready: fixture.sourceDocument.sourceDisplayName,
    },
    { heading: "Trial balance", link: "Trial balance", ready: fixture.accounts[0]?.name },
    { heading: "Banking", link: "Banking", ready: "Reconciled" },
    { heading: "GST & BAS", link: "GST & BAS", ready: fixture.sourceDocument.supplierName },
  ] as const;
  for (const [index, target] of targets.entries()) {
    await page.getByRole("link", { name: target.link }).click();
    await expect(page.getByRole("heading", { level: 1, name: target.heading })).toBeVisible();
    if (!target.ready) throw new Error("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    await expect(page.getByText(target.ready, { exact: true }).first()).toBeVisible();
    if (target.heading === "Banking") {
      await page.getByLabel("Opening balance").fill("1000.00");
      await page
        .getByLabel("CSV rows")
        .fill("2024-05-12,HARBOUR OFFICE SUPPLIES WCS-2024Q4-001,-319.00");
    }
    await expect(page.getByText(/^(?:Loading|Checking)\b/iu)).toHaveCount(0);
    const definition = fixture.screenshots[index];
    if (!definition) throw new Error("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    expect(normalizeAccessibilitySnapshot(await page.locator("body").ariaSnapshot())).toEqual(
      definition.completeAccessibilityText,
    );
  }
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
  const userData = await electronHarness.application.evaluate(({ app, dialog, Menu }) => {
    dialog.showMessageBox = async () => ({ response: 0, checkboxChecked: false });
    const menu = Menu.getApplicationMenu();
    const item = menu?.getMenuItemById("tammy-integrations-pair");
    if (!item) throw new Error("PAIRED_API_MENU_MISSING");
    item.click(item, null, {} as KeyboardEvent);
    return app.getPath("userData");
  });
  const connectionPath = path.join(userData, "local-integrations/connection.json");
  await expect
    .poll(async () =>
      readFile(connectionPath).then(
        () => true,
        () => false,
      ),
    )
    .toBe(true);
  const accounts = await callPairedApi(connectionPath, "tammy.accounting.list_accounts");
  expect(accounts).toMatchObject({
    ok: true,
    data: {
      accounts: expect.arrayContaining([
        expect.objectContaining({ name: fixture.accounts[0]?.name }),
      ]),
    },
  });
  const documents = await callPairedApi(connectionPath, "tammy.documents.list_metadata");
  expect(documents).toMatchObject({
    ok: true,
    data: {
      documents: [
        expect.objectContaining({ sourceDisplayName: fixture.sourceDocument.sourceDisplayName }),
      ],
    },
  });
  expect(JSON.stringify(documents)).not.toContain("extractedText");
  const mcp = await runPairedMcpSmoke(connectionPath);
  expect(mcp.tools).toMatchObject({
    tools: expect.arrayContaining([
      expect.objectContaining({ name: "tammy.accounting.list_accounts" }),
    ]),
  });
  expect(mcp.result).not.toHaveProperty("isError", true);
  expect(JSON.stringify(mcp.result)).toContain(fixture.accounts[0]?.name);
  expect(JSON.stringify(mcp.resource)).toContain(fixture.business.legalName);
  await electronHarness.application.evaluate(({ Menu }) => {
    const item = Menu.getApplicationMenu()?.getMenuItemById("tammy-integrations-revoke");
    if (!item) throw new Error("PAIRED_API_MENU_MISSING");
    item.click(item, null, {} as KeyboardEvent);
  });
  await expect
    .poll(async () =>
      readFile(connectionPath).then(
        () => false,
        () => true,
      ),
    )
    .toBe(true);
});

import { readFile } from "node:fs/promises";
import path from "node:path";
